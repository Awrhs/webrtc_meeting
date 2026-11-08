import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch } from 'react-redux'
import { Card, Form, Input, Button, message, Space, Typography } from 'antd'
import { LoginOutlined, UserOutlined, LockOutlined } from '@ant-design/icons'
import { login } from '@/store/slices/authSlice'
import { AppDispatch } from '@/store'

const { Title } = Typography

export default function Login() {
  const [loading, setLoading] = useState(false)
  const dispatch = useDispatch<AppDispatch>()
  const navigate = useNavigate()

  const onFinish = async (values: { account: string; password: string }) => {
    setLoading(true)
    try {
      await dispatch(login(values)).unwrap()
      message.success('登录成功')
      navigate('/')
    } catch (error: any) {
      const text: string = error?.message || error || '登录失败'
      // 服务端要求先验证邮箱：直接带到验证页
      if (typeof text === 'string' && text.includes('not verified')) {
        message.warning('邮箱尚未验证，请先完成验证')
        const email = values.account.includes('@') ? values.account : ''
        navigate(`/verify-email${email ? `?email=${encodeURIComponent(email)}` : ''}`)
        return
      }
      message.error(text)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      }}
    >
      <Card
        style={{ width: 400, boxShadow: '0 4px 12px rgba(0,0,0,0.15)' }}
        title={
          <div style={{ textAlign: 'center' }}>
            <Title level={3} style={{ margin: 0 }}>
              登录
            </Title>
          </div>
        }
      >
        <Form
          name="login"
          onFinish={onFinish}
          size="large"
          layout="vertical"
        >
          <Form.Item
            name="account"
            rules={[{ required: true, message: '请输入账号' }]}
          >
            <Input
              prefix={<UserOutlined />}
              placeholder="手机号/邮箱/用户名"
            />
          </Form.Item>

          <Form.Item
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="密码"
            />
          </Form.Item>

          <Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              icon={<LoginOutlined />}
            >
              登录
            </Button>
          </Form.Item>

          <div style={{ textAlign: 'center' }}>
            <Space>
              <span>还没有账号？</span>
              <Button type="link" onClick={() => navigate('/register')}>
                立即注册
              </Button>
              <Button type="link" onClick={() => navigate('/forgot-password')}>
                忘记密码？
              </Button>
            </Space>
          </div>
        </Form>
      </Card>
    </div>
  )
}
