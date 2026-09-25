import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { login } from "./helpers";

let noteID = "";
let workspaceAvailable = false;
let ownerContext: BrowserContext;
let ownerPage: Page;

test.beforeAll(async ({ browser }) => {
  ownerContext = await browser.newContext();
  ownerPage = await ownerContext.newPage();
  await login(ownerPage);
  await ownerPage.goto("/show-notes");
  await ownerPage.locator('form[action="/show-notes"] button').click();
  await ownerPage.waitForURL(/\/show-notes\/[0-9a-f-]{36}$/);
  noteID = ownerPage.url().split("/").pop() || "";
  workspaceAvailable = (await ownerPage.locator("[data-show-workspace]").count()) === 1;
});

test.afterAll(async () => {
  if (noteID && ownerPage) {
    await ownerPage.request.delete(`/api/show-notes/${noteID}`).catch(() => {});
  }
  await ownerContext?.close();
});

test.beforeEach(() => {
  test.skip(!workspaceAvailable, "SHOW_NOTES_WORKSPACE_V2 is disabled");
});

test("live preview, slash commands, layouts, and pop-out routes", async () => {
  const editor = ownerPage.locator(".cm-content");
  await editor.click();
  await ownerPage.keyboard.type("# Cold open\n\n1. [Opener](rewind://video/00000000-0000-0000-0000-000000000000) @ 0:18");
  const mediaCard = ownerPage.locator(".sn-media-card");
  await expect(mediaCard).toBeVisible();

  // Pushed room updates must not replace a stable media widget. Replacing it
  // made action buttons flicker and lose their local pending state.
  await ownerPage.waitForTimeout(3_500);
  await mediaCard.evaluate((element) => { element.dataset.stabilityProbe = "preserved"; });
  await ownerPage.waitForTimeout(3_500);
  await expect(mediaCard).toHaveAttribute("data-stability-probe", "preserved");
  await expect(ownerPage.locator(".sn-reference-link").first()).toHaveCSS("color", "rgb(253, 230, 138)");

  const planningRatio = await ownerPage.evaluate(() => {
    const grid = document.querySelector(".show-workspace-grid")?.getBoundingClientRect();
    const room = document.querySelector('[data-workspace-panel="room"]')?.getBoundingClientRect();
    return grid && room ? room.width / grid.width : 0;
  });
  expect(planningRatio).toBeGreaterThan(0.16);
  expect(planningRatio).toBeLessThan(0.21);

  await ownerPage.keyboard.press("Control+End");
  await ownerPage.keyboard.type("\n/break");
  await ownerPage.keyboard.press("Enter");
  await expect(editor).toContainText("Break");

  await ownerPage.locator('[data-layout="recording"]').click();
  await expect(ownerPage.locator('[data-workspace-panel="notes"]')).toBeVisible();
  await expect(ownerPage.locator('[data-workspace-panel="call"]')).toBeVisible();
  await expect(ownerPage.locator('[data-workspace-panel="program"]')).toBeVisible();
  await expect(ownerPage.locator('[data-workspace-panel="room"]')).toBeHidden();
  await expect(ownerPage.locator("[data-call-idle]")).toBeVisible();
  await expect(ownerPage.locator("[data-call-state]")).toHaveText("Call is off");
  expect(await ownerPage.evaluate(() => (window as any).__rewindRoom?.isJoined())).toBe(false);

  await expect(
    ownerPage.locator('[data-workspace-panel="notes"] a[title="Pop out panel"]'),
  ).toHaveAttribute("href", `/show-notes/${noteID}/panel/notes`);
});

test("room events arrive over SSE without a browser polling loop", async ({ browser }) => {
  const peerContext = await browser.newContext();
  const peerPage = await peerContext.newPage();
  const token = `sse-${Date.now()}`;
  try {
    await login(peerPage);
    await peerPage.goto(`/show-notes/${noteID}`);
    const response = await peerPage.request.post(`/api/show-notes/${noteID}/messages`, {
      data: { body: token },
    });
    expect(response.ok()).toBe(true);
    await expect(ownerPage.locator("[data-room-messages]")).toContainText(token);
  } finally {
    await peerContext.close();
  }
});

test("call devices and signaling stay off until explicitly started", async () => {
  await ownerPage.goto(`/show-notes/${noteID}?layout=recording`);
  await expect(ownerPage.locator("[data-call-idle]")).toBeVisible();
  expect(await ownerPage.evaluate(() => (window as any).__rewindRoom?.isJoined())).toBe(false);

  await ownerPage.locator("[data-call-start]").click();
  await expect(ownerPage.locator("[data-call-controls]")).toBeVisible();
  expect(await ownerPage.evaluate(() => (window as any).__rewindRoom?.isJoined())).toBe(true);

  await ownerPage.locator("[data-call-leave]").click();
  await expect(ownerPage.locator("[data-call-idle]")).toBeVisible();
  await expect(ownerPage.locator("[data-call-state]")).toHaveText("Call is off");
  expect(await ownerPage.evaluate(() => (window as any).__rewindRoom?.isJoined())).toBe(false);
});

test("navbar collapses at the container breakpoint", async () => {
  try {
    // navigation.css switches from the menu button to inline destinations at
    // a 70rem container width. Keep both sides of that boundary explicit so a
    // viewport change cannot silently reintroduce stacked labels.
    await ownerPage.setViewportSize({ width: 1000, height: 900 });
    await ownerPage.goto(`/show-notes/${noteID}`);
    await expect(ownerPage.locator("[data-nav-toggle]")).toBeVisible();
    await expect(ownerPage.locator('a.site-nav-direct[href="/jobs"]')).toBeHidden();

    await ownerPage.setViewportSize({ width: 1200, height: 900 });
    await expect(ownerPage.locator("[data-nav-toggle]")).toBeHidden();
    await expect(ownerPage.locator('a.site-nav-direct[href="/jobs"]')).toBeVisible();
  } finally {
    await ownerPage.setViewportSize({ width: 1280, height: 900 });
  }
});

test("two editors converge and undo remains local", async ({ browser }) => {
  const peerContext = await browser.newContext();
  const peerPage = await peerContext.newPage();
  try {
    await login(peerPage);
    await peerPage.goto(`/show-notes/${noteID}`);
    const ownerEditor = ownerPage.locator(".cm-content");
    const peerEditor = peerPage.locator(".cm-content");
    await expect(peerEditor).toBeVisible();

    await ownerEditor.click();
    await ownerPage.keyboard.press("Control+End");
    await ownerPage.keyboard.type("\nowner-only-token");
    await expect(peerEditor).toContainText("owner-only-token");

    await peerEditor.click();
    await peerPage.keyboard.press("Control+End");
    await peerPage.keyboard.type("\npeer-token");
    await expect(ownerEditor).toContainText("peer-token");

    await ownerEditor.click();
    await ownerPage.keyboard.press("Control+z");
    await expect(ownerEditor).not.toContainText("owner-only-token");
    await expect(ownerEditor).toContainText("peer-token");
    await expect(peerEditor).toContainText("peer-token");
  } finally {
    await peerContext.close();
  }
});

test("legacy live route selects the directing layout", async () => {
  await ownerPage.goto(`/show-notes/${noteID}/live`);
  await expect(ownerPage).toHaveURL(new RegExp(`/show-notes/${noteID}\\?layout=directing$`));
  await expect(ownerPage.locator('[data-workspace-panel="program"]')).toBeVisible();
  await expect(ownerPage.locator('[data-workspace-panel="controls"]')).toBeVisible();
});
