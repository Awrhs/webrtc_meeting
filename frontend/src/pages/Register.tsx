import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch } from 'react-redux'
import { Card, Form, Input, Button, message, Space, Typography } from 'antd'
import { UserAddOutlined, UserOutlined, LockOutlined, MailOutlined, PhoneOutlined } from '@ant-design/icons'
import { register } from '@/store/slices/authSlice'
import { AppDispatch } from '@/store'

const { Title } = Typography

export default function Register() {
  const [loading, setLoading] = useState(false)
  const dispatch = useDispatch<AppDispatch>()
  const navigate = useNavigate()

  const onFinish = async (values: { 
    username: string
    email: string
    phone: string
    password: string
  }) => {
    setLoading(true)
    try {
      await dispatch(register(values)).unwrap()
      message.success('注册成功，请登录')
      navigate('/login')
    } catch (error: any) {
      message.error(error.message || error || '注册失败')
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
        style={{ width: 450, boxShadow: '0 4px 12px rgba(0,0,0,0.15)' }}
        title={
          <div style={{ textAlign: 'center' }}>
            <Title level={3} style={{ margin: 0 }}>
              注册账号
            </Title>
          </div>
        }
      >
        <Form
          name="register"
          onFinish={onFinish}
          size="large"
          layout="vertical"
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[
              { required: true, message: '请输入用户名' },
              { min: 3, max: 20, message: '用户名长度3-20位' },
            ]}
          >
            <Input
              prefix={<UserOutlined />}
              placeholder="请输入用户名"
            />
          </Form.Item>

          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效的邮箱地址' },
            ]}
          >
            <Input
              prefix={<MailOutlined />}
              placeholder="请输入邮箱"
            />
          </Form.Item>

          <Form.Item
            name="phone"
            label="手机号"
            rules={[{ required: true, message: '请输入手机号' }]}
          >
            <Input
              prefix={<PhoneOutlined />}
              placeholder="请输入手机号"
            />
          </Form.Item>

          <Form.Item
            name="password"
            label="密码"
            rules={[
              { required: true, message: '请输入密码' },
              { min: 8, max: 20, message: '密码长度8-20位' },
            ]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="请输入密码"
            />
          </Form.Item>

          <Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              icon={<UserAddOutlined />}
            >
              注册
            </Button>
          </Form.Item>

          <div style={{ textAlign: 'center' }}>
            <Space>
              <span>已有账号？</span>
              <Button type="link" onClick={() => navigate('/login')}>
                立即登录
              </Button>
            </Space>
          </div>
        </Form>
      </Card>
    </div>
  )
}
