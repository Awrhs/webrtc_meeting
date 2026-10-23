import { test, expect } from '@playwright/test'

// 冒烟套件：应用外壳可加载（不依赖真实后端）。
// CI 中用静态预览服务器跑：npm run build && npx playwright test
test('unauthenticated user is redirected to the login page', async ({ page }) => {
  await page.goto('/')
  // 未登录应跳转到 /login（ProtectedRoute）
  await page.waitForURL(/\/login/, { timeout: 15000 })
  await expect(page.getByPlaceholder(/手机号|账号/)).toBeVisible()
})

test('PWA manifest is served', async ({ request }) => {
  const response = await request.get('/manifest.webmanifest')
  expect(response.ok()).toBeTruthy()
  const body = await response.json()
  expect(body.name).toContain('SFU')
})

test('register page is reachable and renders its form', async ({ page }) => {
  await page.goto('/register')
  await expect(page.getByPlaceholder(/请输入用户名|用户名/)).toBeVisible()
  await expect(page.getByPlaceholder(/邮箱/)).toBeVisible()
})

test.describe('full meeting E2E (requires E2E_FULL=1 + backend stack)', () => {
  test.skip(!process.env.E2E_FULL, 'set E2E_FULL=1 and run make e2e-up to enable')

  test('two users register, create room, join and exchange media', async ({ browser }) => {
    const suffix = String(Date.now()).slice(-8)

    // 1) Alice 注册 + 建房
    const aliceContext = await browser.newContext({ permissions: ['camera', 'microphone'] })
    const alice = await aliceContext.newPage()
    await alice.goto('/register')
    await alice.getByPlaceholder(/请输入用户名|用户名/).fill(`alice${suffix}`)
    await alice.getByPlaceholder(/邮箱/).fill(`alice${suffix}@example.com`)
    await alice.getByPlaceholder(/手机号|手机/).fill(`139${suffix}`)
    await alice.getByPlaceholder(/密码/).first().fill('Password123')
    await alice.click('button[type="submit"]')

    await alice.waitForURL('/', { timeout: 20000 })
    await alice.click('text=创建房间')
    await alice.getByPlaceholder(/请输入房间名称|房间名称/).fill(`房间${suffix}`)
    await alice.getByRole('button', { name: /确定|创建/ }).click()

    await alice.waitForURL(/\/meeting\/\d+/, { timeout: 20000 })
    // 默认纯音频入会：开启摄像头
    await alice.locator('button').filter({ has: alice.locator('.anticon-video-camera') }).first().click()

    // 2) Bob 注册 + 加入
    const bobContext = await browser.newContext({ permissions: ['camera', 'microphone'] })
    const bob = await bobContext.newPage()
    await bob.goto('/register')
    await bob.getByPlaceholder(/请输入用户名|用户名/).fill(`bob${suffix}`)
    await bob.getByPlaceholder(/邮箱/).fill(`bob${suffix}@example.com`)
    await bob.getByPlaceholder(/手机号|手机/).fill(`138${suffix}`)
    await bob.getByPlaceholder(/密码/).first().fill('Password123')
    await bob.click('button[type="submit"]')
    await bob.waitForURL('/', { timeout: 20000 })

    // 3) 两端都应看到对方（成员列表）与媒体连接成功提示
    await expect(alice.getByText(`bob${suffix}`)).toBeVisible({ timeout: 30000 })
    await expect(bob.getByText(`alice${suffix}`)).toBeVisible({ timeout: 30000 })
    await expect(alice.getByText('媒体连接已建立')).toBeVisible({ timeout: 30000 })
    await expect(bob.getByText('媒体连接已建立')).toBeVisible({ timeout: 30000 })
  })
})
