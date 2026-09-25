/**
 * Read-only browser smoke tests for the authenticated application shell.
 *
 * These checks deliberately stay on GET routes. They verify that the release
 * can render its primary navigation destinations without touching videos,
 * clips, jobs, or other archive state.
 */
import { test, expect } from "@playwright/test";
import { login } from "./helpers";

const NAVIGATION_ROUTES = [
  "/",
  "/videos",
  "/channels",
  "/creators",
  "/follows",
  "/jobs",
  "/wiki",
  "/investigate",
  "/stitch",
  "/show-notes",
  "/visual",
  "/upload",
  "/settings",
];

test.describe("Authenticated navigation smoke", () => {
  test("primary destinations render without server or page errors", async ({
    page,
  }) => {
    await login(page);
    const pageErrors: Error[] = [];
    page.on("pageerror", (error) => pageErrors.push(error));

    for (const route of NAVIGATION_ROUTES) {
      const response = await page.goto(route, { waitUntil: "domcontentloaded" });
      expect(response, `${route} did not return a document`).not.toBeNull();
      expect(response?.status(), `${route} did not return a successful document`).toBe(200);
      expect(new URL(page.url()).pathname, `${route} redirected unexpectedly`).toBe(route);
      await expect(page.locator("#main-nav"), `${route} missing main navigation`).toBeVisible();
      await expect(page.locator("#page-content"), `${route} missing page content`).toBeVisible();
      // Let deferred module initialization and DataStar boot handlers settle
      // before checking for uncaught browser errors.
      await page.waitForTimeout(250);
    }

    expect(pageErrors, "uncaught browser errors while navigating").toEqual([]);
  });

  test("responsive navigation opens and closes with keyboard controls", async ({ page }) => {
    await login(page);
    await page.setViewportSize({ width: 1000, height: 800 });
    await page.goto("/videos", { waitUntil: "domcontentloaded" });

    const toggle = page.locator("[data-nav-toggle]");
    const menu = page.locator("#site-nav-menu");
    await expect(toggle).toBeVisible();
    await expect(menu).toBeHidden();

    await toggle.click();
    await expect(menu).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");

    await page.locator("#main-nav").press("Escape");
    await expect(menu).toBeHidden();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
  });

  test("network reaches graph readiness on the disposable fixture", async ({ page }) => {
    test.skip(
      process.env.E2E_NETWORK_SMOKE !== "1",
      "set E2E_NETWORK_SMOKE=1 against the disposable fixture to run the network readiness smoke",
    );

    await login(page);
    const pageErrors: Error[] = [];
    page.on("pageerror", (error) => pageErrors.push(error));
    const response = await page.goto("/network", {
      waitUntil: "domcontentloaded",
      timeout: 15_000,
    });
    expect(response, "/network did not return a document").not.toBeNull();
    expect(response?.status(), "/network did not return a successful document").toBe(200);
    expect(new URL(page.url()).pathname).toBe("/network");
    await expect(page.locator("#network-graph-data")).toBeAttached({ timeout: 5_000 });
    await expect(page.locator("#network-count")).toContainText("creators / channels", {
      timeout: 5_000,
    });
    await expect(page.locator("#main-nav")).toBeVisible();
    await expect(page.locator("#page-content")).toBeVisible();
    expect(pageErrors, "uncaught browser errors on /network").toEqual([]);
  });
});
