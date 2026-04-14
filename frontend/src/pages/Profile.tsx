import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Card, Form, Input, Button, Avatar, message, Typography, Space, Divider } from 'antd'
import { UserOutlined, UploadOutlined, LockOutlined } from '@ant-design/icons'
import { updateProfile, getMe } from '@/store/slices/authSlice'
import { RootState, AppDispatch } from '@/store'

const { Header, Content } = Layout
const { Title } = Typography

export default function Profile() {
  const dispatch = useDispatch<AppDispatch>()
  const navigate = useNavigate()
  const { user } = useSelector((state: RootState) => state.auth)
  const [formProfile] = Form.useForm()
  const [formPassword] = Form.useForm()
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    dispatch(getMe())
  }, [dispatch])

  useEffect(() => {
    if (user) {
      formProfile.setFieldsValue({
        nickname: user.nickname,
        avatar: user.avatar,
      })
    }
  }, [user, formProfile])

  const handleUpdateProfile = async (values: { nickname: string; avatar: string }) => {
    setLoading(true)
    try {
      await dispatch(updateProfile(values)).unwrap()
      message.success('个人资料更新成功')
    } catch (error: any) {
      message.error(error)
    } finally {
      setLoading(false)
    }
  }

  const handleBack = () => {
    navigate('/')
  }

  return (
    <Layout>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          background: 'white',
          padding: '0 24px',
          boxShadow: '0 1px 4px rgba(0,0,0,0.1)',
        }}
      >
        <Button onClick={handleBack}>返回房间列表</Button>
        <Title level={4} style={{ margin: '0 0 0 16px' }}>
          个人中心
        </Title>
      </Header>
      <Content
        style={{
          padding: '24px',
          background: '#f5f5f5',
          minHeight: 'calc(100vh - 64px)',
        }}
      >
        <Card style={{ maxWidth: 600, margin: '0 auto' }}>
          <div style={{ textAlign: 'center', marginBottom: 24 }}>
            <Avatar size={80} src={user?.avatar} icon={<UserOutlined />} />
            <Title level={5} style={{ marginTop: 16, marginBottom: 0 }}>
              {user?.nickname}
            </Title>
            <div style={{ color: '#888' }}>{user?.email}</div>
          </div>

          <Divider />

          <Title level={5}>基本资料</Title>
          <Form
            form={formProfile}
            layout="vertical"
            onFinish={handleUpdateProfile}
          >
            <Form.Item
              name="nickname"
              label="昵称"
              rules={[{ required: true, min: 2, max: 10, message: '昵称长度2-10位' }]}
            >
              <Input placeholder="请输入昵称" />
            </Form.Item>

            <Form.Item
              name="avatar"
              label="头像URL"
            >
              <Input placeholder="请输入头像URL" />
            </Form.Item>

            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading} block>
                保存修改
              </Button>
            </Form.Item>
          </Form>

          <Divider />

          <Title level={5}>账号信息</Title>
          <div style={{ padding: '16px 0' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
              <span style={{ color: '#666' }}>用户名</span>
              <span>{user?.username}</span>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
              <span style={{ color: '#666' }}>邮箱</span>
              <span>{user?.email}</span>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <span style={{ color: '#666' }}>手机号</span>
              <span>{user?.phone}</span>
            </div>
          </div>
        </Card>
      </Content>
    </Layout>
  )
}
