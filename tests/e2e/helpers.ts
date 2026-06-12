/**
 * Shared helpers for rewind E2E tests.
 *
 * The test suite assumes the full Docker Compose stack is running locally.
 * It also assumes at least one video is present in the database so we can
 * navigate to a cut page.
 */
import { type Page, expect } from "@playwright/test";

// ---------------------------------------------------------------------------
// Navigation helpers
// ---------------------------------------------------------------------------

/** Discover a video ID from the library and return it. */
export async function getFirstVideoID(page: Page): Promise<string> {
  await page.goto("/videos");
  // The video list renders <a> links with href like /videos/{uuid}
  const link = page.locator('a[href^="/videos/"]').first();
  await expect(link).toBeVisible({ timeout: 10_000 });
  const href = await link.getAttribute("href");
  if (!href) throw new Error("No video link found on /videos");
  // Extract UUID — the href is /videos/{uuid}
  const match = href.match(/\/videos\/([0-9a-f-]{36})/);
  if (!match) throw new Error(`Unexpected video link format: ${href}`);
  return match[1];
}

/** Navigate to the cut page for a given video. */
export async function goToCutPage(page: Page, videoID: string) {
  await page.goto(`/videos/${videoID}/cut`);
  // Wait for DataStar to initialise — the cut page container has data-cut-page
  await expect(page.locator("[data-cut-page]")).toBeVisible({ timeout: 10_000 });
}

// ---------------------------------------------------------------------------
// Auth helpers
// ---------------------------------------------------------------------------

const TEST_USER = "e2e_test";
const TEST_PASS = "e2e_test_password";
const TEST_EMAIL = "e2e@test.local";

/**
 * Ensure the test user exists by attempting registration, then log in.
 * Registration is attempted once; if the user already exists the form
 * will show an error, which we ignore and proceed to login.
 */
export async function login(page: Page) {
  // 1. Try to register the test user (idempotent — fails silently if exists)
  await page.goto("/register");
  if (page.url().includes("/register")) {
    await page.fill('input[name="username"]', TEST_USER);
    await page.fill('input[name="email"]', TEST_EMAIL);
    await page.fill('input[name="password"]', TEST_PASS);
    await page.fill('input[name="confirm_password"]', TEST_PASS);
    await page.click('button[type="submit"]');
    // Wait briefly for the response — might redirect (success) or stay (error)
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  }

  // 2. If registration succeeded we may already be logged in — check
  if (!page.url().includes("/login") && !page.url().includes("/register")) {
    return; // already authenticated after registration
  }

  // 3. Log in
  await page.goto("/login");
  if (!page.url().includes("/login")) {
    return; // already authenticated (session cookie)
  }
  await page.fill('input[name="username"]', TEST_USER);
  await page.fill('input[name="password"]', TEST_PASS);
  await page.click('button[type="submit"]');
  await page.waitForURL((url) => !url.pathname.includes("/login"), {
    timeout: 10_000,
  });
}

// ---------------------------------------------------------------------------
// Filter stack helpers
// ---------------------------------------------------------------------------

/** Open the "COLOR / FILTERS" sidebar panel if it's collapsed. */
export async function openFiltersPanel(page: Page) {
  const stack = page.locator("#filter-stack");
  if (await stack.isVisible()) return;

  // The sidebar panel button contains "COLOR / FILTERS" text
  const panelBtn = page.locator("button", { hasText: "COLOR / FILTERS" });
  await panelBtn.click();
  await expect(stack).toBeVisible({ timeout: 5_000 });
}

/**
 * Add a filter by type name (e.g. "brightness", "contrast").
 * Opens the "Add Filter" dropdown, clicks the matching button, and waits
 * for the SSE morph to add the card.
 */
export async function addFilter(page: Page, filterType: string) {
  // Count existing cards before adding
  const before = await page.locator(".filter-card").count();

  // Open dropdown
  const addBtn = page.locator("#filter-stack summary");
  await addBtn.click();

  // Click the filter button — match by the data-on:click attribute containing
  // the filter type name
  const filterBtn = page.locator(
    `#filter-stack button[data-on\\:click*="type:'${filterType}'"]`,
  );
  await expect(filterBtn).toBeVisible({ timeout: 2_000 });

  // Click and wait for the SSE round-trip
  const responsePromise = page.waitForResponse(
    (resp) => resp.url().includes("filter-cards"),
  );
  await filterBtn.click();
  await responsePromise;

  // Wait for SSE morph to add the new card
  await expect(page.locator(".filter-card")).toHaveCount(before + 1, {
    timeout: 5_000,
  });
  // Let DataStar process morphed elements' data-on:* attributes
  await page.waitForTimeout(250);
}

/** Remove filter at a given index (0-based). Waits for SSE morph. */
export async function removeFilter(page: Page, index: number) {
  const before = await page.locator(".filter-card").count();
  const card = page.locator(`#filter-card-${index}`);
  await expect(card).toBeVisible();
  const removeBtn = card.locator('button[title="Remove filter"]');

  const responsePromise = page.waitForResponse(
    (resp) => resp.url().includes("filter-cards"),
  );
  await removeBtn.click();
  await responsePromise;

  await expect(page.locator(".filter-card")).toHaveCount(before - 1, {
    timeout: 5_000,
  });
  // Let DataStar process morphed elements' data-on:* attributes
  await page.waitForTimeout(250);
}

/** Get the number of filters currently in the stack. */
export async function filterCount(page: Page): Promise<number> {
  return page.locator(".filter-card").count();
}

/**
 * Read the current value of a range slider param on a filter card.
 * @param index - filter card index (0-based)
 * @param paramKey - the param key (e.g. "value", "gain")
 */
export async function getSliderValue(
  page: Page,
  index: number,
  paramKey: string,
): Promise<string> {
  const card = page.locator(`#filter-card-${index}`);
  // The slider input has data-on:input containing the param key
  const slider = card.locator(
    `input[type="range"][data-on\\:input*="${paramKey}"]`,
  );
  return (await slider.inputValue()) || "";
}

/**
 * Set a range slider on a filter card to a specific value.
 * Directly modifies the DataStar signal via the exposed __dsAPI, which
 * is more reliable than dispatching synthetic DOM events through the
 * reactive proxy layer.
 */
export async function setSliderValue(
  page: Page,
  index: number,
  paramKey: string,
  value: number,
) {
  await page.evaluate(
    ({ idx, key, val }) => {
      const api = (window as any).__dsAPI;
      const stack = api?.getPath("_filterStack");
      if (stack && stack[idx]) {
        if (!stack[idx].params) stack[idx].params = {};
        stack[idx].params[key] = String(val);
      }
    },
    { idx: index, key: paramKey, val: value },
  );
}

/**
 * Wait for an SSE morph cycle to complete after a filter stack mutation.
 * Verifies the expected number of cards is reached.
 */
export async function waitForFilterCards(page: Page, expectedCount: number) {
  await expect(page.locator(".filter-card")).toHaveCount(expectedCount, {
    timeout: 5_000,
  });
}

/**
 * Get the filter type label from a card at a given index.
 * Returns the visible label text (e.g. "Brightness", "Contrast").
 */
export async function getFilterLabel(
  page: Page,
  index: number,
): Promise<string> {
  const card = page.locator(`#filter-card-${index}`);
  const label = card.locator(".filter-card-header span.font-semibold");
  return (await label.textContent())?.trim() || "";
}
