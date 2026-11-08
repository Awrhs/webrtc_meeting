import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Form, Input, Button, message, Space, Typography, Steps } from 'antd'
import { MailOutlined, LockOutlined, SafetyOutlined, UserOutlined } from '@ant-design/icons'
import { api } from '@/utils/api'

const { Title, Text } = Typography

// 找回密码：第一步输入账号发送验证码，第二步输入验证码与新密码。
// 服务端对不存在的账号同样返回成功（不泄露账号是否存在），所以提示语统一为"若账号存在..."。
export default function ForgotPassword() {
  const navigate = useNavigate()
  const [step, setStep] = useState<0 | 1>(0)
  const [account, setAccount] = useState('')
  const [loading, setLoading] = useState(false)

  const requestCode = async (values: { account: string }) => {
    setLoading(true)
    try {
      await api.post('/api/v1/auth/password/forgot', { account: values.account.trim() })
      setAccount(values.account.trim())
      setStep(1)
      message.success('若该账号存在，验证码已发送到其绑定邮箱（15 分钟内有效）')
    } catch (error: any) {
      message.error(error?.response?.data?.error || '发送验证码失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  const resetPassword = async (values: { code: string; new_password: string; confirm: string }) => {
    if (values.new_password !== values.confirm) {
      message.error('两次输入的新密码不一致')
      return
    }
    setLoading(true)
    try {
      await api.post('/api/v1/auth/password/reset', {
        account,
        code: values.code.trim(),
        new_password: values.new_password,
      })
      message.success('密码已重置，请使用新密码登录')
      navigate('/login')
    } catch (error: any) {
      message.error(error?.response?.data?.error || '重置失败，请检查验证码')
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
        style={{ width: 440, boxShadow: '0 4px 12px rgba(0,0,0,0.15)' }}
        title={
          <div style={{ textAlign: 'center' }}>
            <Title level={3} style={{ margin: 0 }}>
              找回密码
            </Title>
          </div>
        }
      >
        <Steps
          size="small"
          current={step}
          items={[{ title: '验证账号' }, { title: '设置新密码' }]}
          style={{ marginBottom: 24 }}
        />

        {step === 0 ? (
          <Form name="forgot" onFinish={requestCode} size="large" layout="vertical">
            <Form.Item name="account" rules={[{ required: true, message: '请输入账号' }]}>
              <Input prefix={<UserOutlined />} placeholder="手机号/邮箱/用户名" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading} block icon={<MailOutlined />}>
                发送验证码
              </Button>
            </Form.Item>
          </Form>
        ) : (
          <Form name="reset" onFinish={resetPassword} size="large" layout="vertical">
            <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
              账号：{account}
            </Text>
            <Form.Item
              name="code"
              rules={[
                { required: true, message: '请输入验证码' },
                { len: 6, message: '验证码为 6 位数字' },
              ]}
            >
              <Input prefix={<SafetyOutlined />} placeholder="6 位邮件验证码" maxLength={6} />
            </Form.Item>
            <Form.Item
              name="new_password"
              rules={[
                { required: true, message: '请输入新密码' },
                { min: 8, max: 20, message: '密码长度8-20位' },
              ]}
            >
              <Input.Password prefix={<LockOutlined />} placeholder="新密码" />
            </Form.Item>
            <Form.Item name="confirm" rules={[{ required: true, message: '请再次输入新密码' }]}>
              <Input.Password prefix={<LockOutlined />} placeholder="确认新密码" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading} block icon={<LockOutlined />}>
                重置密码
              </Button>
            </Form.Item>
            <div style={{ textAlign: 'center' }}>
              <Button type="link" onClick={() => requestCode({ account })} disabled={loading}>
                重新发送验证码
              </Button>
            </div>
          </Form>
        )}

        <div style={{ textAlign: 'center' }}>
          <Space>
            <span>想起密码了？</span>
            <Button type="link" onClick={() => navigate('/login')}>
              返回登录
            </Button>
          </Space>
        </div>
      </Card>
    </div>
  )
}
