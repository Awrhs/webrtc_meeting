import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Button, Result, Spin } from 'antd'
import { AppDispatch, RootState } from '@/store'
import { getMe, logout } from '@/store/slices/authSlice'

interface ProtectedRouteProps {
  children: React.ReactNode
}

// 受保护路由：未登录跳转登录页；已有 token 但还没有用户资料（刷新页面、直接打开会议链接）
// 时先拉取 /user/me 再渲染子页面。否则会议页会以 user.id=0 运行：自己被当成远端成员渲染、
// 找不到自己的成员记录与权限、自己的 media_state 不会被过滤、聊天归属全部错位。
export default function ProtectedRoute({ children }: ProtectedRouteProps) {
  const dispatch = useDispatch<AppDispatch>()
  const { isAuthenticated, user } = useSelector((state: RootState) => state.auth)
  const [loadFailed, setLoadFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!isAuthenticated || user) return
    let cancelled = false
    setLoadFailed(false)
    dispatch(getMe())
      .unwrap()
      .catch(() => {
        if (!cancelled) setLoadFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [isAuthenticated, user, dispatch, attempt])

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  if (!user) {
    if (loadFailed) {
      return (
        <Result
          status="warning"
          title="无法加载账号信息"
          subTitle="请检查网络后重试，或重新登录"
          extra={[
            <Button type="primary" key="retry" onClick={() => setAttempt(a => a + 1)}>
              重试
            </Button>,
            <Button key="relogin" onClick={() => dispatch(logout())}>
              重新登录
            </Button>,
          ]}
        />
      )
    }
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '100vh',
          gap: 12,
        }}
      >
        <Spin size="large" />
        <span>正在加载账号信息...</span>
      </div>
    )
  }

  return <>{children}</>
}
