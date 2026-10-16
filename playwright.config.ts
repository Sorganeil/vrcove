import {defineConfig,devices} from '@playwright/test';

const origin=`http://localhost:${process.env.VRCOVE_TEST_FRONTEND_PORT||15173}`;

export default defineConfig({
 testDir:'./tests/frontend',
 fullyParallel:false,
 workers:1,
 timeout:30_000,
 use:{baseURL:origin,trace:'retain-on-failure',launchOptions:{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}},
 projects:[{name:'desktop',use:{...devices['Desktop Chrome']}},{name:'phone',use:{...devices['iPhone 13'],defaultBrowserType:'chromium'}}],
 webServer:{command:'node scripts/test-frontend-server.mjs',url:origin+'/api/stats',reuseExistingServer:false,timeout:60_000},
});
