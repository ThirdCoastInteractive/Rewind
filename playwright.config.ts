import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false, // tests share a single running instance
  retries: 0,
  workers: 1,
  reporter: "list",
  timeout: 30_000,

  use: {
    baseURL: `http://localhost:${process.env.WEBSERVER_PORT || "9115"}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
});
