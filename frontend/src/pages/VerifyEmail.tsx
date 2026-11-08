import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Card, Form, Input, Button, message, Space, Typography } from 'antd'
import { MailOutlined, SafetyOutlined } from '@ant-design/icons'
import { api } from '@/utils/api'

const { Title, Text } = Typography

// 邮箱验证：注册后（服务端开启 REQUIRE_EMAIL_VERIFICATION 时）输入邮件里的 6 位验证码。
export default function VerifyEmail() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [loading, setLoading] = useState(false)
  const [resending, setResending] = useState(false)
  const initialEmail = searchParams.get('email') || ''

  const verify = async (values: { email: string; code: string }) => {
    setLoading(true)
    try {
      await api.post('/api/v1/auth/email/verify', { email: values.email.trim(), code: values.code.trim() })
      message.success('邮箱验证成功，请登录')
      navigate('/login')
    } catch (error: any) {
      message.error(error?.response?.data?.error || '验证失败，请检查验证码')
    } finally {
      setLoading(false)
    }
  }

  const resend = async (email: string) => {
    if (!email) {
      message.error('请先填写邮箱')
      return
    }
    setResending(true)
    try {
      await api.post('/api/v1/auth/email/resend', { email: email.trim() })
      message.success('若该邮箱已注册且未验证，新的验证码已发送')
    } catch (error: any) {
      message.error(error?.response?.data?.error || '发送失败，请稍后重试')
    } finally {
      setResending(false)
    }
  }

  const [form] = Form.useForm()

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
        style={{ width: 440, boxShadow: '0 4px 12px rgba(0,0,0,0.15)' }}
        title={
          <div style={{ textAlign: 'center' }}>
            <Title level={3} style={{ margin: 0 }}>
              验证邮箱
            </Title>
          </div>
        }
      >
        <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
          我们已向你的邮箱发送了 6 位验证码（24 小时内有效）。请输入验证码完成注册。
        </Text>
        <Form form={form} name="verify-email" onFinish={verify} size="large" layout="vertical" initialValues={{ email: initialEmail }}>
          <Form.Item
            name="email"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效的邮箱地址' },
            ]}
          >
            <Input prefix={<MailOutlined />} placeholder="注册邮箱" />
          </Form.Item>
          <Form.Item
            name="code"
            rules={[
              { required: true, message: '请输入验证码' },
              { len: 6, message: '验证码为 6 位数字' },
            ]}
          >
            <Input prefix={<SafetyOutlined />} placeholder="6 位邮件验证码" maxLength={6} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block icon={<SafetyOutlined />}>
              完成验证
            </Button>
          </Form.Item>
        </Form>
        <div style={{ textAlign: 'center' }}>
          <Space>
            <Button type="link" loading={resending} onClick={() => resend(form.getFieldValue('email'))}>
              重新发送验证码
            </Button>
            <Button type="link" onClick={() => navigate('/login')}>
              返回登录
            </Button>
          </Space>
        </div>
      </Card>
    </div>
  )
}
