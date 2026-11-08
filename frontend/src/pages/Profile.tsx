import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Card, Form, Input, Button, Avatar, message, Typography, Divider, List, Tag, Popconfirm } from 'antd'
import { UserOutlined, LaptopOutlined, DeleteOutlined } from '@ant-design/icons'
import { updateProfile, getMe, setToken, logout } from '@/store/slices/authSlice'
import { clearLegacyKeyStore } from '@/utils/encryption'
import { api } from '@/utils/api'
import { getDeviceId } from '@/utils/device'
import { RootState, AppDispatch } from '@/store'
import { Device } from '@/types'

const { Header, Content } = Layout
const { Title } = Typography

export default function Profile() {
  const dispatch = useDispatch<AppDispatch>()
  const navigate = useNavigate()
  const { user } = useSelector((state: RootState) => state.auth)
  const [formProfile] = Form.useForm()
  const [formPassword] = Form.useForm()
  const [loading, setLoading] = useState(false)
  const [passwordLoading, setPasswordLoading] = useState(false)
  // 登录设备管理
  const [devices, setDevices] = useState<Device[]>([])
  const [devicesLoading, setDevicesLoading] = useState(false)
  const currentDeviceId = getDeviceId()

  const loadDevices = useCallback(async () => {
    setDevicesLoading(true)
    try {
      const response = await api.get('/api/v1/user/devices')
      setDevices(response.data?.devices || [])
    } catch {
      message.error('获取设备列表失败')
    } finally {
      setDevicesLoading(false)
    }
  }, [])

  // 删除设备 = 吊销该设备上的全部登录会话。删的是当前设备时，本地也会随之登出。
  const handleDeleteDevice = async (device: Device) => {
    try {
      await api.delete(`/api/v1/user/devices/${device.id}`)
      if (device.device_id === currentDeviceId) {
        message.success('已删除当前设备并退出登录')
        dispatch(logout())
        navigate('/login')
        return
      }
      message.success('设备已删除，该设备上的登录已失效')
      void loadDevices()
    } catch {
      message.error('删除设备失败')
    }
  }

  // 改密码：服务端会让所有旧会话失效，并为当前设备返回新 token，这里直接替换，不用重新登录
  const handleChangePassword = async (values: { old_password: string; new_password: string; confirm: string }) => {
    if (values.new_password !== values.confirm) {
      message.error('两次输入的新密码不一致')
      return
    }
    setPasswordLoading(true)
    try {
      const response = await api.put('/api/v1/user/password', {
        old_password: values.old_password,
        new_password: values.new_password,
      })
      if (response.data?.token) {
        dispatch(setToken(response.data.token))
      }
      formPassword.resetFields()
      message.success('密码已修改，其它设备上的登录已全部失效')
    } catch (error: any) {
      message.error(error?.response?.data?.error || '修改密码失败')
    } finally {
      setPasswordLoading(false)
    }
  }

  useEffect(() => {
    dispatch(getMe())
  }, [dispatch])

  useEffect(() => {
    void loadDevices()
  }, [loadDevices])

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

          <Divider />

          <Title level={5}>修改密码</Title>
          <div style={{ color: '#999', fontSize: 12, marginBottom: 8 }}>
            修改后其它设备上的登录会全部失效，当前设备保持登录。
          </div>
          <Form form={formPassword} layout="vertical" onFinish={handleChangePassword}>
            <Form.Item name="old_password" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}>
              <Input.Password placeholder="当前密码" />
            </Form.Item>
            <Form.Item
              name="new_password"
              label="新密码"
              rules={[
                { required: true, message: '请输入新密码' },
                { min: 8, max: 20, message: '密码长度8-20位' },
              ]}
            >
              <Input.Password placeholder="新密码（8-20 位）" />
            </Form.Item>
            <Form.Item name="confirm" label="确认新密码" rules={[{ required: true, message: '请再次输入新密码' }]}>
              <Input.Password placeholder="再次输入新密码" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={passwordLoading} block>
                修改密码
              </Button>
            </Form.Item>
          </Form>

          <Divider />

          <Title level={5}>登录设备</Title>
          <div style={{ color: '#999', fontSize: 12, marginBottom: 8 }}>
            每次登录会记录设备（浏览器设备 ID）、UA 与 IP。删除设备会立即吊销该设备上的登录会话。
          </div>
          <List
            loading={devicesLoading}
            dataSource={devices}
            locale={{ emptyText: '暂无设备记录' }}
            renderItem={(device) => (
              <List.Item
                actions={[
                  <Popconfirm
                    key="delete"
                    title={device.device_id === currentDeviceId ? '删除当前设备将退出登录，确定？' : '删除该设备并吊销其登录会话？'}
                    onConfirm={() => handleDeleteDevice(device)}
                  >
                    <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
                  </Popconfirm>,
                ]}
              >
                <List.Item.Meta
                  avatar={<LaptopOutlined style={{ fontSize: 24, color: '#1677ff' }} />}
                  title={
                    <span>
                      {device.device_name || device.device_id.slice(0, 12)}
                      <Tag style={{ marginLeft: 8 }}>{device.ip}</Tag>
                      {device.device_id === currentDeviceId && <Tag color="green">当前设备</Tag>}
                    </span>
                  }
                  description={
                    <span style={{ fontSize: 12 }}>
                      {(device.user_agent || '').slice(0, 80)}
                      {device.last_active ? ` · 最近活跃：${new Date(device.last_active).toLocaleString()}` : ''}
                    </span>
                  }
                />
              </List.Item>
            )}
          />

          <Divider />

          <Title level={5}>安全</Title>
          <Popconfirm
            title="清除本设备保存的 E2EE 密钥？"
            description="清除后历史加密消息将无法解密，需要重新进行密钥交换。"
            onConfirm={() => {
              clearLegacyKeyStore()
              message.success('已清除本设备的 E2EE 密钥数据')
            }}
          >
            <Button danger>清除本设备 E2EE 密钥</Button>
          </Popconfirm>
        </Card>
      </Content>
    </Layout>
  )
}
