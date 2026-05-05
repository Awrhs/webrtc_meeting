import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Button, Card, List, Input, Space, Tag, Modal, Form, message, Typography, Avatar, Dropdown } from 'antd'
import { PlusOutlined, LockOutlined, UnlockOutlined, UserOutlined, SettingOutlined, LogoutOutlined, CopyOutlined, DeleteOutlined, InfoCircleOutlined, EyeOutlined } from '@ant-design/icons'
import { getMyRooms, createRoom, joinRoom, getRoomByNumber } from '@/store/slices/roomSlice'
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
  const [joinByIdModalVisible, setJoinByIdModalVisible] = useState(false)
  const [joinVerifyCode, setJoinVerifyCode] = useState('')
  const [joinRoomId, setJoinRoomId] = useState('')
  const [newRoomInfo, setNewRoomInfo] = useState<Room | null>(null)
  const [inviteModalVisible, setInviteModalVisible] = useState(false)
  const [loading, setLoading] = useState(false)
  const [selectedRoom, setSelectedRoom] = useState<Room | null>(null)
  const [manageRoomModalVisible, setManageRoomModalVisible] = useState(false)
  const [manageRoom, setManageRoom] = useState<Room | null>(null)
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

      if (result.auth_type === 'invite_code' && result.invite_code) {
        setNewRoomInfo(result)
        setInviteModalVisible(true)
      } else {
        navigate(`/meeting/${result.id}`)
      }
    } catch (error: any) {
      message.error(error.message || error || '创建失败')
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
      navigate(`/meeting/${room.id}`)
    }
  }

  const handleConfirmJoin = async () => {
    if (selectedRoom) {
      setLoading(true)
      try {
        await dispatch(joinRoom({
          id: selectedRoom.id.toString(),
          data: { verify_code: joinVerifyCode }
        })).unwrap()
        message.success('加入成功')
        setJoinModalVisible(false)
        navigate(`/meeting/${selectedRoom.id}`)
      } catch (error: any) {
        message.error(error?.message || error || '验证失败')
      } finally {
        setLoading(false)
      }
    }
  }

  const handleJoinById = async () => {
    if (!joinRoomId.trim()) {
      message.error('请输入房间ID')
      return
    }

    setLoading(true)
    try {
      // 先获取房间信息
      const room = await dispatch(getRoomByNumber(joinRoomId)).unwrap()

      if (room.auth_type !== 'none') {
        // 需要验证
        setSelectedRoom(room)
        setJoinVerifyCode('')
        setJoinByIdModalVisible(false)
        setJoinModalVisible(true)
      } else {
        // 不需要验证，直接加入
        await dispatch(joinRoom({ id: room.id.toString(), data: {} })).unwrap()
        message.success('加入成功')
        setJoinByIdModalVisible(false)
        navigate(`/meeting/${room.id}`)
      }
    } catch (error: any) {
      message.error(error?.message || error || '房间不存在或加入失败')
    } finally {
      setLoading(false)
    }
  }

  const handleManageRoom = (room: Room, e: React.MouseEvent) => {
    e.stopPropagation()
    setManageRoom(room)
    setManageRoomModalVisible(true)
  }

  const handleCopyRoomId = () => {
    if (manageRoom) {
      navigator.clipboard.writeText(manageRoom.room_id)
      message.success('房间ID已复制')
    }
  }

  const handleCopyInviteCode = () => {
    if (manageRoom?.invite_code) {
      navigator.clipboard.writeText(manageRoom.invite_code)
      message.success('邀请码已复制')
    }
  }

  const handleDeleteRoom = () => {
    if (!manageRoom) return
    Modal.confirm({
      title: '确认解散房间',
      content: '解散后所有成员将被移出，且无法恢复，确定解散吗？',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          const token = localStorage.getItem('token')
          await fetch(`/api/v1/rooms/${manageRoom.id}`, {
            method: 'DELETE',
            headers: {
              Authorization: `Bearer ${token}`,
            },
          })
          message.success('房间已解散')
          setManageRoomModalVisible(false)
          dispatch(getMyRooms())
        } catch (error: any) {
          message.error('操作失败')
        }
      },
    })
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
            <Button onClick={() => setJoinByIdModalVisible(true)}>
              加入房间
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
                  actions={room.owner_id === user?.id ? [
                    <SettingOutlined
                      key="manage"
                      onClick={(e) => handleManageRoom(room, e)}
                    />,
                  ] : []}
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
        title={`请输入${selectedRoom?.auth_type === 'password' ? '房间密码' : '邀请码'}`}
        open={joinModalVisible}
        okText="加入"
        cancelText="取消"
        onOk={handleConfirmJoin}
        onCancel={() => setJoinModalVisible(false)}
      >
        <Input
          placeholder={`请输入${selectedRoom?.auth_type === 'password' ? '房间密码' : '房间邀请码'}`}
          value={joinVerifyCode}
          onChange={(e) => setJoinVerifyCode(e.target.value)}
        />
      </Modal>

      <Modal
        title="房间创建成功"
        open={inviteModalVisible}
        onCancel={() => {
          setInviteModalVisible(false)
          if (newRoomInfo) {
            navigate(`/meeting/${newRoomInfo.id}`)
          }
        }}
        onOk={() => {
          if (newRoomInfo) {
            navigator.clipboard.writeText(newRoomInfo.invite_code || '')
            message.success('邀请码已复制到剪贴板')
            navigate(`/meeting/${newRoomInfo.id}`)
          }
        }}
        okText="复制并进入房间"
      >
        <div style={{ textAlign: 'center', padding: '20px 0' }}>
          <p style={{ marginBottom: 16 }}>您的房间邀请码是：</p>
          <div style={{
            fontSize: 32,
            fontWeight: 'bold',
            letterSpacing: 4,
            color: '#1890ff',
            padding: '20px',
            background: '#f5f5f5',
            borderRadius: 8
          }}>
            {newRoomInfo?.invite_code}
          </div>
          <p style={{ marginTop: 16, color: '#666' }}>
            请将此邀请码分享给需要加入会议的成员
          </p>
        </div>
      </Modal>

      <Modal
        title="加入房间"
        open={joinByIdModalVisible}
        okText="加入"
        cancelText="取消"
        onOk={handleJoinById}
        onCancel={() => setJoinByIdModalVisible(false)}
        confirmLoading={loading}
      >
        <Input
          placeholder="请输入房间ID（6位数字）"
          value={joinRoomId}
          onChange={(e) => setJoinRoomId(e.target.value)}
          maxLength={6}
        />
      </Modal>

      <Modal
        title="房间管理"
        open={manageRoomModalVisible}
        onCancel={() => setManageRoomModalVisible(false)}
        footer={[
          <Button key="close" onClick={() => setManageRoomModalVisible(false)}>
            关闭
          </Button>,
          <Button key="delete" danger onClick={handleDeleteRoom}>
            解散房间
          </Button>,
        ]}
      >
        {manageRoom && (
          <div style={{ padding: '10px 0' }}>
            <div style={{ marginBottom: 20 }}>
              <div style={{ fontWeight: 'bold', marginBottom: 8 }}>房间名称</div>
              <div style={{ color: '#666' }}>{manageRoom.name}</div>
            </div>
            <div style={{ marginBottom: 20 }}>
              <div style={{ fontWeight: 'bold', marginBottom: 8 }}>房间ID</div>
              <Space>
                <code style={{ background: '#f5f5f5', padding: '4px 8px', borderRadius: 4 }}>
                  {manageRoom.room_id}
                </code>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={handleCopyRoomId}
                >
                  复制
                </Button>
              </Space>
            </div>
            {manageRoom.auth_type === 'invite_code' && manageRoom.invite_code && (
              <div style={{ marginBottom: 20 }}>
                <div style={{ fontWeight: 'bold', marginBottom: 8 }}>邀请码</div>
                <Space>
                  <code style={{ background: '#f5f5f5', padding: '4px 8px', borderRadius: 4 }}>
                    {manageRoom.invite_code}
                  </code>
                  <Button
                    size="small"
                    icon={<CopyOutlined />}
                    onClick={handleCopyInviteCode}
                  >
                    复制
                  </Button>
                </Space>
              </div>
            )}
            <div style={{ marginBottom: 20 }}>
              <div style={{ fontWeight: 'bold', marginBottom: 8 }}>房间状态</div>
              <Space>
                <Tag color={manageRoom.is_locked ? 'red' : 'green'}>
                  {manageRoom.is_locked ? '已锁定' : '开放中'}
                </Tag>
                <Tag color="blue">
                  {manageRoom.current_size}/{manageRoom.max_members} 人
                </Tag>
              </Space>
            </div>
            <div style={{ color: '#999', fontSize: 12 }}>
              <InfoCircleOutlined style={{ marginRight: 4 }} />
              解散房间后，所有成员将被移出，且操作无法撤销。
            </div>
          </div>
        )}
      </Modal>
    </Layout>
  )
}
