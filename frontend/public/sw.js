// 极简 PWA Service Worker：仅预缓存应用外壳（同源静态资源），
// 对 API/WebSocket 请求一律直连网络。安装失败或缓存不可用时自动降级为无 SW。
const CACHE_NAME = 'conference-sfu-shell-v1'
const SHELL_ASSETS = ['/', '/manifest.webmanifest', '/icon.svg']

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_ASSETS)).catch(() => undefined)
  )
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    )
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  // 只处理同源 GET；API/WS/媒体流一律直连
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/uploads/')) return

  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) return cached
      return fetch(request).then((response) => {
        // 成功的基础资源写入缓存（网络优先、失败回退外壳）
        if (response.ok && (request.destination === 'document' || request.destination === 'style' || request.destination === 'script')) {
          const clone = response.clone()
          caches.open(CACHE_NAME).then((cache) => cache.put(request, clone)).catch(() => undefined)
        }
        return response
      }).catch(() => caches.match('/'))
    })
  )
})
