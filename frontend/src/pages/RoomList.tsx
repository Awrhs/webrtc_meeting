import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Button, Card, List, Input, Space, Tag, Modal, Form, message, Typography, Avatar, Dropdown } from 'antd'
import { PlusOutlined, LockOutlined, UnlockOutlined, UserOutlined, SettingOutlined, LogoutOutlined } from '@ant-design/icons'
import { getMyRooms, createRoom } from '@/store/slices/roomSlice'
import { logout } from '@/store/slices/authSlice'
import { RootState, AppDispatch } from '@/store'
import { Room } from '@/types'

const { Header, Content } = Layout
const { Title } = Typography

export default function RoomList() {
  const dispatch = useDispatch<AppDispatch>()
  const navigate = useNavigate()
  const { user } = useSelector((state: RootState) => state.auth)
  const { rooms } = useSelector((state: RootState) => state.room)
  const [searchText, setSearchText] = useState('')
  const [createModalVisible, setCreateModalVisible] = useState(false)
  const [joinModalVisible, setJoinModalVisible] = useState(false)
  const [joinVerifyCode, setJoinVerifyCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [selectedRoom, setSelectedRoom] = useState<Room | null>(null)
  const [form] = Form.useForm()

  useEffect(() => {
    dispatch(getMyRooms())
  }, [dispatch])

  const filteredRooms = rooms.filter(room =>
    room.name.toLowerCase().includes(searchText.toLowerCase()) ||
    room.room_id.includes(searchText)
  )

  const handleCreateRoom = async (values: any) => {
    setLoading(true)
    try {
      const result = await dispatch(createRoom(values)).unwrap()
      message.success('创建成功')
      setCreateModalVisible(false)
      form.resetFields()
      navigate(`/room/${result.id}`)
    } catch (error: any) {
      message.error(error)
    } finally {
      setLoading(false)
    }
  }

  const handleJoinRoom = (room: Room) => {
    if (room.auth_type !== 'none') {
      setSelectedRoom(room)
      setJoinVerifyCode('')
      setJoinModalVisible(true)
    } else {
      navigate(`/room/${room.id}`)
    }
  }

  const handleConfirmJoin = () => {
    if (selectedRoom) {
      navigate(`/room/${selectedRoom.id}?verify=${joinVerifyCode}`)
    }
  }

  const userMenuItems = [
    {
      key: 'profile',
      icon: <SettingOutlined />,
      label: '个人设置',
      onClick: () => navigate('/profile'),
    },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      danger: true,
      onClick: () => {
        dispatch(logout())
        message.success('已退出登录')
        navigate('/login')
      },
    },
  ]

  return (
    <Layout>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          background: 'white',
          padding: '0 24px',
          boxShadow: '0 1px 4px rgba(0,0,0,0.1)',
        }}
      >
        <Title level={4} style={{ margin: 0 }}>
          SFU音视频会议
        </Title>
        <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
          <Space style={{ cursor: 'pointer' }}>
            <Avatar src={user?.avatar} icon={<UserOutlined />} />
            <span>{user?.nickname}</span>
          </Space>
        </Dropdown>
      </Header>
      <Content
        style={{
          padding: '24px',
          background: '#f5f5f5',
          minHeight: 'calc(100vh - 64px)',
        }}
      >
        <Card style={{ marginBottom: 24 }}>
          <Space style={{ marginBottom: 16 }}>
            <Input.Search
              placeholder="搜索房间ID或名称"
              allowClear
              style={{ width: 300 }}
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
              onSearch={(value) => setSearchText(value)}
            />
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setCreateModalVisible(true)}
            >
              创建房间
            </Button>
          </Space>

          <List
            grid={{ gutter: 16, column: 4, xxl: 4, xl: 3, lg: 3, md: 2, sm: 1, xs: 1 }}
            dataSource={filteredRooms}
            renderItem={(room) => (
              <List.Item>
                <Card
                  hoverable
                  onClick={() => handleJoinRoom(room)}
                  style={{ height: '100%' }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'start',
                      marginBottom: 8,
                    }}
                  >
                    <Title level={5} style={{ margin: 0 }}>
                      {room.name}
                    </Title>
                    {room.is_locked ? (
                      <LockOutlined style={{ color: '#f5222d' }} />
                    ) : (
                      <UnlockOutlined style={{ color: '#52c41a' }} />
                    )}
                  </div>
                  {room.topic && (
                    <div style={{ color: '#666', marginBottom: 8 }}>
                      {room.topic}
                    </div>
                  )}
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginTop: 12,
                    }}
                  >
                    <Space size="small">
                      <Tag color="blue">ID: {room.room_id}</Tag>
                    </Space>
                    <span style={{ color: '#888', fontSize: 12 }}>
                      {room.current_size}/{room.max_members}
                    </span>
                  </div>
                </Card>
              </List.Item>
            )}
          />
        </Card>
      </Content>

      <Modal
        title="创建会议房间"
        open={createModalVisible}
        onCancel={() => setCreateModalVisible(false)}
        footer={null}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleCreateRoom}
        >
          <Form.Item
            name="name"
            label="房间名称"
            rules={[{ required: true, min: 2, max: 20, message: '房间名称长度2-20位' }]}
          >
            <Input placeholder="请输入房间名称" />
          </Form.Item>

          <Form.Item
            name="topic"
            label="会议主题"
            rules={[{ max: 50, message: '会议主题最多50位' }]}
          >
            <Input placeholder="请输入会议主题（可选）" />
          </Form.Item>

          <Form.Item
            name="max_members"
            label="人数上限"
            initialValue={50}
            rules={[{ required: true }]}
          >
            <Input type="number" min={1} max={500} />
          </Form.Item>

          <Form.Item
            name="auth_type"
            label="验证方式"
            initialValue="none"
          >
            <select
              style={{
                width: '100%',
                padding: '4px 11px',
                border: '1px solid #d9d9d9',
                borderRadius: '6px',
              }}
            >
              <option value="none">无验证</option>
              <option value="password">密码验证</option>
              <option value="invite_code">邀请码验证</option>
            </select>
          </Form.Item>

          <Form.Item
            noStyle
            shouldUpdate={(prev, curr) => prev.auth_type !== curr.auth_type}
          >
            {({ getFieldValue }) =>
              getFieldValue('auth_type') === 'password' ? (
                <Form.Item
                  name="password"
                  label="房间密码"
                  rules={[{ required: true, min: 6, max: 12, message: '密码长度6-12位' }]}
                >
                  <Input.Password placeholder="请设置房间密码" />
                </Form.Item>
              ) : null
            }
          </Form.Item>

          <Form.Item
            name="is_permanent"
            label="房间类型"
            valuePropName="checked"
            initialValue={false}
          >
            <div>
              <label>
                <input type="checkbox" /> 永久房间（临时房间无人时自动销毁）
              </label>
            </div>
          </Form.Item>

          <Form.Item>
            <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
              <Button onClick={() => setCreateModalVisible(false)}>取消</Button>
              <Button type="primary" htmlType="submit" loading={loading}>
                创建
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="请输入验证信息"
        open={joinModalVisible}
        okText="加入"
        cancelText="取消"
        onOk={handleConfirmJoin}
        onCancel={() => setJoinModalVisible(false)}
      >
        <Input
          placeholder="请输入验证信息"
          value={joinVerifyCode}
          onChange={(e) => setJoinVerifyCode(e.target.value)}
        />
      </Modal>
    </Layout>
  )
}
