import { Component, ReactNode } from 'react'
import { Result, Button } from 'antd'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error?: Error
}

export default class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: { componentStack: string }) {
    console.error('[ErrorBoundary] Caught error:', error, errorInfo)
  }

  handleReload = () => {
    window.location.reload()
  }

  handleGoHome = () => {
    window.location.href = '/'
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      return (
        <Result
          status="error"
          title="页面遇到了一些问题"
          subTitle={this.state.error?.message || '未知错误'}
          extra={[
            <Button type="primary" key="reload" onClick={this.handleReload}>
              刷新页面
            </Button>,
            <Button key="home" onClick={this.handleGoHome}>
              返回首页
            </Button>,
          ]}
        />
      )
    }

    return this.props.children
  }
}
