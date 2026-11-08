// 极简 PWA Service Worker：只负责应用外壳的离线可用，API/WebSocket/上传一律直连网络。
//
// 缓存策略（按请求类型区分）：
//  - 导航请求（HTML 文档）：network-first。联网时永远取最新的 index.html，
//    这样发布新版本后用户立刻拿到新的资源清单；离线时回退到缓存的外壳 '/'。
//  - /assets/ 下的构建产物：Vite 输出的文件名带内容 hash，内容永不变化，cache-first 安全。
//  - 其它同源静态资源（manifest、图标）：network-first，失败回退缓存。
//
// 【历史教训】旧版本对文档和脚本也用 cache-first，且缓存名固定不变、本文件内容不变，
// 浏览器永远不会安装新 SW、也不会清缓存，用户访问过的页面会被永久钉在旧版本上。
const CACHE_NAME = 'conference-sfu-shell-v2'
const SHELL_ASSETS = ['/', '/manifest.webmanifest', '/icon.svg']

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_ASSETS)).catch(() => undefined)
  )
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

// 只缓存同源且成功的响应；写缓存失败（配额不足等）不影响返回
const putInCache = (request, response) => {
  if (!response || !response.ok || response.type !== 'basic') return
  const clone = response.clone()
  caches.open(CACHE_NAME).then((cache) => cache.put(request, clone)).catch(() => undefined)
}

const networkFirst = async (request, fallbackUrl) => {
  try {
    const response = await fetch(request)
    putInCache(request, response)
    return response
  } catch {
    const cached = await caches.match(request)
    if (cached) return cached
    if (fallbackUrl) {
      const fallback = await caches.match(fallbackUrl)
      if (fallback) return fallback
    }
    return Response.error()
  }
}

const cacheFirst = async (request) => {
  const cached = await caches.match(request)
  if (cached) return cached
  const response = await fetch(request)
  putInCache(request, response)
  return response
}

self.addEventListener('fetch', (event) => {
  const request = event.request
  // 只处理同源 GET；API、上传文件、WebSocket 一律不经过 SW
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/uploads/') || url.pathname === '/ws') return

  if (request.mode === 'navigate') {
    // SPA：任何路由的导航离线时都回退到缓存的 '/'（index.html）
    event.respondWith(networkFirst(request, '/'))
    return
  }
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(cacheFirst(request))
    return
  }
  event.respondWith(networkFirst(request))
})
