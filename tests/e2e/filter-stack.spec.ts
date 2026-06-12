/**
 * E2E tests for the Cut page filter stack.
 *
 * Prerequisites:
 * - The full Docker Compose stack is running (`make up`)
 * - At least one video exists in the database
 * - The app is accessible at WEBSERVER_PORT (default 9115)
 *
 * Run with:
 *   pnpm exec playwright test tests/e2e/filter-stack.spec.ts
 */
import { test, expect } from "@playwright/test";
import {
  login,
  getFirstVideoID,
  goToCutPage,
  openFiltersPanel,
  addFilter,
  removeFilter,
  filterCount,
  getSliderValue,
  setSliderValue,
  getFilterLabel,
  waitForFilterCards,
} from "./helpers";

let videoID: string;

test.beforeAll(async ({ browser }) => {
  const page = await browser.newPage();
  await login(page);
  videoID = await getFirstVideoID(page);
  await page.close();
});

test.describe("Filter Stack — Add / Remove", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await goToCutPage(page, videoID);
    await openFiltersPanel(page);
  });

  test("add a single filter", async ({ page }) => {
    expect(await filterCount(page)).toBe(0);
    await addFilter(page, "brightness");
    expect(await filterCount(page)).toBe(1);
    expect(await getFilterLabel(page, 0)).toBe("Brightness");
  });

  test("add multiple filters in sequence", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");
    await addFilter(page, "saturation");

    expect(await filterCount(page)).toBe(3);
    expect(await getFilterLabel(page, 0)).toBe("Brightness");
    expect(await getFilterLabel(page, 1)).toBe("Contrast");
    expect(await getFilterLabel(page, 2)).toBe("Saturation");
  });

  test("remove a filter from the end", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");
    expect(await filterCount(page)).toBe(2);

    await removeFilter(page, 1); // remove contrast
    expect(await filterCount(page)).toBe(1);
    expect(await getFilterLabel(page, 0)).toBe("Brightness");
  });

  test("remove a filter from the beginning", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");
    expect(await filterCount(page)).toBe(2);

    await removeFilter(page, 0); // remove brightness
    expect(await filterCount(page)).toBe(1);
    expect(await getFilterLabel(page, 0)).toBe("Contrast");
  });

  test("remove all filters one by one", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");
    await addFilter(page, "saturation");

    await removeFilter(page, 2);
    expect(await filterCount(page)).toBe(2);

    await removeFilter(page, 1);
    expect(await filterCount(page)).toBe(1);

    await removeFilter(page, 0);
    expect(await filterCount(page)).toBe(0);
  });

  test("add filter after removing all filters", async ({ page }) => {
    await addFilter(page, "brightness");
    await removeFilter(page, 0);
    expect(await filterCount(page)).toBe(0);

    await addFilter(page, "contrast");
    expect(await filterCount(page)).toBe(1);
    expect(await getFilterLabel(page, 0)).toBe("Contrast");
  });
});

test.describe("Filter Stack — Parameter Persistence", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await goToCutPage(page, videoID);
    await openFiltersPanel(page);
  });

  test("slider value survives adding another filter", async ({ page }) => {
    await addFilter(page, "brightness");

    // Change slider value
    await setSliderValue(page, 0, "value", 0.5);

    // Add another filter — the brightness value should NOT reset
    await addFilter(page, "contrast");

    expect(await filterCount(page)).toBe(2);
    expect(await getFilterLabel(page, 0)).toBe("Brightness");

    // The brightness slider should still show the adjusted value
    const val = await getSliderValue(page, 0, "value");
    expect(parseFloat(val)).toBeCloseTo(0.5, 1);
  });

  test("slider value survives removing a different filter", async ({
    page,
  }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");

    // Set brightness to non-default value
    await setSliderValue(page, 0, "value", 0.3);
    // Set contrast to non-default value
    await setSliderValue(page, 1, "value", 1.8);

    // Remove brightness (index 0) — contrast values should persist
    await removeFilter(page, 0);

    expect(await filterCount(page)).toBe(1);
    expect(await getFilterLabel(page, 0)).toBe("Contrast");

    const contrastVal = await getSliderValue(page, 0, "value");
    expect(parseFloat(contrastVal)).toBeCloseTo(1.8, 1);
  });

  test("default slider values are correct on add", async ({ page }) => {
    await addFilter(page, "brightness");
    const val = await getSliderValue(page, 0, "value");
    // Default brightness should be 0
    expect(parseFloat(val)).toBeCloseTo(0, 1);
  });
});

test.describe("Filter Stack — Reorder", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await goToCutPage(page, videoID);
    await openFiltersPanel(page);
  });

  test("move filter down", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");

    // Click "Move down" on brightness (index 0)
    const card0 = page.locator("#filter-card-0");
    const moveDownBtn = card0.locator('button[title="Move down"]');
    await moveDownBtn.click();

    // Wait for re-render
    await page.waitForTimeout(500);

    expect(await getFilterLabel(page, 0)).toBe("Contrast");
    expect(await getFilterLabel(page, 1)).toBe("Brightness");
  });

  test("move filter up", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");

    // Click "Move up" on contrast (index 1)
    const card1 = page.locator("#filter-card-1");
    const moveUpBtn = card1.locator('button[title="Move up"]');
    await moveUpBtn.click();

    await page.waitForTimeout(500);

    expect(await getFilterLabel(page, 0)).toBe("Contrast");
    expect(await getFilterLabel(page, 1)).toBe("Brightness");
  });

  test("reorder preserves parameter values", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");

    // Set specific values
    await setSliderValue(page, 0, "value", 0.3);
    await setSliderValue(page, 1, "value", 1.5);

    // Move brightness down
    const card0 = page.locator("#filter-card-0");
    const moveDownBtn = card0.locator('button[title="Move down"]');
    await moveDownBtn.click();

    await page.waitForTimeout(500);

    // Now contrast is at index 0, brightness at index 1
    expect(await getFilterLabel(page, 0)).toBe("Contrast");
    expect(await getFilterLabel(page, 1)).toBe("Brightness");

    // Values should be preserved
    const contrastVal = await getSliderValue(page, 0, "value");
    expect(parseFloat(contrastVal)).toBeCloseTo(1.5, 1);

    const brightnessVal = await getSliderValue(page, 1, "value");
    expect(parseFloat(brightnessVal)).toBeCloseTo(0.3, 1);
  });
});

test.describe("Filter Stack — Rapid Operations (Bug Regression)", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await goToCutPage(page, videoID);
    await openFiltersPanel(page);
  });

  test("rapid add: two filters added quickly", async ({ page }) => {
    // Add two filters in rapid succession without waiting for SSE
    const addBtn = page.locator("#filter-stack summary");

    await addBtn.click();
    await page
      .locator(`#filter-stack button[data-on\\:click*="type:'brightness'"]`)
      .click();

    // Immediately add another without waiting for the first to render
    await addBtn.click();
    await page
      .locator(`#filter-stack button[data-on\\:click*="type:'contrast'"]`)
      .click();

    // Both should appear
    await waitForFilterCards(page, 2);
    expect(await getFilterLabel(page, 0)).toBe("Brightness");
    expect(await getFilterLabel(page, 1)).toBe("Contrast");
  });

  test("add then immediately remove", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");

    // Remove first and immediately add another
    await removeFilter(page, 0);
    await addFilter(page, "saturation");

    expect(await filterCount(page)).toBe(2);
    expect(await getFilterLabel(page, 0)).toBe("Contrast");
    expect(await getFilterLabel(page, 1)).toBe("Saturation");
  });

  test("adjust slider then remove different filter", async ({ page }) => {
    await addFilter(page, "brightness");
    await addFilter(page, "contrast");
    await addFilter(page, "saturation");

    // Set saturation to a specific value
    await setSliderValue(page, 2, "value", 2.0);

    // Remove brightness (index 0) — saturation should keep its value
    await removeFilter(page, 0);

    // Saturation is now at index 1
    expect(await getFilterLabel(page, 1)).toBe("Saturation");
    const satVal = await getSliderValue(page, 1, "value");
    expect(parseFloat(satVal)).toBeCloseTo(2.0, 1);
  });
});
