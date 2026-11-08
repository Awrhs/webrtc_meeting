import axios from 'axios'
import { getDeviceId } from './device'

// API 基地址：与 signaling.ts 保持一致地使用 VITE_API_HOST。
// 旧实现 baseURL 为 ''（相对路径），在 Vite dev server 和 Docker 前端容器上
// 都没有 /api 反向代理，登录等全部 API 调用必然 404。
const API_BASE_URL = import.meta.env.VITE_API_HOST || 'http://localhost:8080'

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  // 设备标识：服务端把登录会话绑定到设备，删除设备即可吊销其会话
  config.headers['X-Device-Id'] = getDeviceId()
  return config
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // 会话失效（登出、改密码、被踢下线、过期）：清理本地凭据并回到登录页。
      // 登录/注册等公开接口返回 401 时不跳转，让页面自行提示。
      const url: string = error.config?.url || ''
      const isPublicAuthEndpoint = url.includes('/auth/login') || url.includes('/auth/register') || url.includes('/auth/password/') || url.includes('/auth/email/')
      if (!isPublicAuthEndpoint) {
        localStorage.removeItem('token')
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

export { api }
