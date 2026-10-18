import axios from 'axios'

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
  return config
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export { api }
