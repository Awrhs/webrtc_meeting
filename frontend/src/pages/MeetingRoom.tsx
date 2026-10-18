import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Button, Space, Tag, message, Modal, Dropdown, MenuProps, Tabs } from 'antd'
import {
  VideoCameraOutlined,
  VideoCameraFilled,
  AudioOutlined,
  AudioMutedOutlined,
  StopOutlined,
  LockOutlined,
  UnlockOutlined,
  DeleteOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { RootState, AppDispatch } from '@/store'
import { joinRoom, getRoomMessages, addMessage, setConnected, clearCurrentRoom, updateMember, removeMember, lockRoom, deleteRoom } from '@/store/slices/roomSlice'
import { MediaE2EEState, MediaE2EEManager } from '@/utils/mediaE2EE'
import VideoGrid from '@/components/VideoGrid'
import Chat from '@/components/Chat'
import MemberList from '@/components/MemberList'
import { JoinRoomResponse, RoomMember } from '@/types'
import { useRemoteStreams } from '@/hooks/useRemoteStreams'
import { useLocalMedia } from '@/hooks/useLocalMedia'
import { useMeetingSignaling } from '@/hooks/useMeetingSignaling'
import { useSFUMedia } from '@/hooks/useSFUMedia'

const { Header, Content } = Layout

export default function MeetingRoom() {
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const dispatch = useDispatch<AppDispatch>()
  const { currentRoom, members, messages, myPermissions } = useSelector((state: RootState) => state.room)
  const { user } = useSelector((state: RootState) => state.auth)

  const [loading, setLoading] = useState(false)
  const [mediaE2EEState, setMediaE2EEState] = useState<MediaE2EEState>('disabled')

  const membersRef = useRef<RoomMember[]>([])
  const localStreamRef = useRef<MediaStream | null>(null)
  const mediaE2EEManagerRef = useRef<MediaE2EEManager | null>(null)
  const leaveMeetingRef = useRef<(() => void) | null>(null)
  const navigateHomeRef = useRef<(() => void) | null>(null)

  const currentUserId = user?.id || 0

  const { remoteStreams, addRemoteStream, removeRemoteStream, removeRemoteStreamsByUser, clearRemoteStreams } = useRemoteStreams()

  const canLockRoom = myPermissions.includes('lock_room')
  const canDeleteRoom = myPermissions.includes('delete_room')

  useEffect(() => {
    membersRef.current = members
  }, [members])

  const getActiveMemberIds = useCallback((excludeUserId?: number) => {
    const ids = membersRef.current.map(member => member.user_id)
    if (excludeUserId === undefined || ids.includes(excludeUserId)) return ids
    return [...ids, excludeUserId]
  }, [])

  const { sfuConnectionRef, connect: sfuConnect, disconnect: sfuDisconnect, setVideoSubscriptions } = useSFUMedia({
    onRemoteTrack: (userId: number, _track: MediaStreamTrack, stream: MediaStream) => {
      // 流归属明确后，将其从 userId=0 的未识别池中移除，避免同一流被渲染两次
      if (userId !== 0) {
        removeRemoteStream(0, stream)
      }
      addRemoteStream(userId, stream)
    },
    mediaE2EEManagerRef,
    localStreamRef,
  })

  // 视频选择性订阅策略：最多订阅前 6 个开启视频的用户
  useEffect(() => {
    const videoUserIds = members
      .filter(m => m.is_video_on && m.user_id !== currentUserId)
      .slice(0, 6)
      .map(m => m.user_id)
    setVideoSubscriptions(videoUserIds)
  }, [members, currentUserId, setVideoSubscriptions])

  const {
    videoOn,
    audioOn,
    screenShare,
    localStream,
    screenStream,
    toggleVideo,
    toggleAudio,
    toggleScreenShare,
    cleanupLocalMedia,
  } = useLocalMedia({ sfuConnectionRef, currentRoomId: currentRoom?.id })

  const signalingDisconnectRef = useRef<(() => void) | null>(null)

  const leaveMeeting = useCallback(() => {
    cleanupLocalMedia()
    sfuDisconnect()
    signalingDisconnectRef.current?.()
    clearRemoteStreams()
    setMediaE2EEState('disabled')
  }, [cleanupLocalMedia, sfuDisconnect, clearRemoteStreams])

  useEffect(() => {
    leaveMeetingRef.current = leaveMeeting
    navigateHomeRef.current = () => navigate('/')
  }, [leaveMeeting, navigate])

  const signaling = useMeetingSignaling({
    dispatch: {
      setConnected: (connected: boolean) => dispatch(setConnected(connected)),
      addMessage: (msg: any) => dispatch(addMessage(msg)),
      updateMember: (member: RoomMember) => dispatch(updateMember(member)),
      removeMember: (userId: number) => dispatch(removeMember(userId)),
    },
    onRemoveRemoteStreams: removeRemoteStreamsByUser,
    getActiveMemberIds,
    currentUserId,
    membersRef,
    leaveMeetingRef,
    navigateHomeRef,
    mediaE2EEManagerRef,
  })

  useEffect(() => {
    signalingDisconnectRef.current = signaling.disconnect
  }, [signaling.disconnect])

  useEffect(() => {
    const verifyCode = searchParams.get('verify') || ''
    const roomId = parseInt(id!)

    setLoading(true)
    dispatch(joinRoom({ id: id!, data: { verify_code: verifyCode } }))
      .unwrap()
      .then(async (result: JoinRoomResponse) => {
        dispatch(getRoomMessages(roomId))
        await startMeeting(result)
      })
      .catch((error: any) => {
        message.error(error)
        navigate('/')
      })
      .finally(() => {
        setLoading(false)
      })

    return () => {
      leaveMeetingRef.current?.()
      dispatch(clearCurrentRoom())
    }
  }, [id, dispatch])

  const startMeeting = async (joinResult: JoinRoomResponse) => {
    await signaling.connectAndJoin(
      parseInt(id!),
      currentUserId,
      joinResult.members,
      setMediaE2EEState
    )

    await sfuConnect(
      parseInt(id!),
      currentUserId,
      joinResult.sfu_token,
    )
  }

  const handleLeave = () => {
    Modal.confirm({
      title: '确认离开会议',
      content: '你确定要离开本次会议吗？',
      onOk: () => {
        leaveMeeting()
        navigate('/')
      },
    })
  }

  const handleLockRoom = async () => {
    if (!currentRoom) return
    try {
      const newLockedState = !currentRoom.is_locked
      await dispatch(lockRoom({ roomId: currentRoom.id, locked: newLockedState })).unwrap()
      message.success(newLockedState ? '房间已锁定' : '房间已解锁')
    } catch (error: any) {
      message.error(error || '操作失败')
    }
  }

  const handleDeleteRoom = () => {
    if (!currentRoom) return
    Modal.confirm({
      title: '确认解散房间',
      content: '解散后所有成员将被移出，且无法恢复，确定解散吗？',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await dispatch(deleteRoom(currentRoom.id)).unwrap()
          message.success('房间已解散')
          leaveMeeting()
          navigate('/')
        } catch (error: any) {
          message.error(error || '操作失败')
        }
      },
    })
  }

  const getMyRemoteStream = () => {
    if (videoOn && localStream) {
      return localStream
    }
    return null
  }

  const buildMenuItems = (): MenuProps['items'] => {
    const items: MenuProps['items'] = []

    if (canLockRoom) {
      items.push({
        key: 'lock',
        icon: currentRoom?.is_locked ? <UnlockOutlined /> : <LockOutlined />,
        label: currentRoom?.is_locked ? '解锁房间' : '锁定房间',
        onClick: handleLockRoom,
      })
    }

    if (canDeleteRoom) {
      items.push({
        key: 'delete',
        icon: <DeleteOutlined />,
        label: '解散房间',
        danger: true,
        onClick: handleDeleteRoom,
      })
    }

    if (items.length > 0) {
      items.push({ type: 'divider' })
    }

    items.push({
      key: 'leave',
      icon: <LogoutOutlined />,
      label: '离开会议',
      onClick: handleLeave,
    })

    return items
  }

  const items = buildMenuItems()

  const mediaE2EETag = () => {
    switch (mediaE2EEState) {
      case 'enabled':
        return <Tag color="green">媒体 E2EE 已启用</Tag>
      case 'negotiating':
        return <Tag color="orange">媒体 E2EE 协商中</Tag>
      case 'unsupported':
        return <Tag color="default">媒体 E2EE 不支持，已回退 DTLS-SRTP</Tag>
      case 'warning':
        return <Tag color="red">媒体 E2EE 需检查</Tag>
      default:
        return <Tag color="blue">媒体传输加密 DTLS-SRTP</Tag>
    }
  }

  if (loading) {
    return <div>加载中...</div>
  }

  return (
    <Layout style={{ height: '100vh' }}>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          background: 'white',
          padding: '0 16px',
          height: 64,
        }}
      >
        <div>
          <span style={{ fontSize: 16, fontWeight: 'bold' }}>{currentRoom?.name}</span>
          {currentRoom?.topic && (
            <span style={{ marginLeft: 16, color: '#888' }}>{currentRoom.topic}</span>
          )}
          <Tag color={currentRoom?.is_locked ? 'red' : 'green'} style={{ marginLeft: 16 }}>
            {currentRoom?.is_locked ? '已锁定' : '开放中'}
          </Tag>
          {mediaE2EETag()}
          <span style={{ marginLeft: 16, color: '#888' }}>
            {currentRoom?.current_size}/{currentRoom?.max_members} 人
          </span>
        </div>
        <Dropdown menu={{ items }} placement="bottomRight">
          <Button type="primary">房间操作</Button>
        </Dropdown>
      </Header>
      <Layout>
        <Content style={{ background: '#000', overflow: 'hidden' }}>
          <VideoGrid
            members={members}
            currentUserId={currentUserId}
            localStream={getMyRemoteStream()}
            screenStream={screenShare ? screenStream : null}
            remoteStreams={remoteStreams}
          />
        </Content>
        <div
          style={{
            width: 350,
            background: 'white',
            borderLeft: '1px solid #f0f0f0',
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          <Tabs
            defaultActiveKey="members"
            items={[
              { key: 'members', label: '成员列表', children: <MemberList roomId={parseInt(id!)} members={members} currentUserId={currentUserId} permissions={myPermissions} /> },
              { key: 'chat', label: '聊天', children: <Chat roomId={parseInt(id!)} messages={messages} signaling={signaling.signalingRef.current} /> },
            ]}
          />
          <div style={{ padding: 16, borderTop: '1px solid #f0f0f0' }}>
            <Space size="middle" style={{ width: '100%', justifyContent: 'center' }}>
              <Button
                type={videoOn ? 'primary' : 'default'}
                shape="circle"
                size="large"
                icon={videoOn ? <VideoCameraFilled /> : <VideoCameraOutlined />}
                onClick={toggleVideo}
              />
              <Button
                type={audioOn ? 'primary' : 'default'}
                danger={!audioOn}
                shape="circle"
                size="large"
                icon={audioOn ? <AudioOutlined /> : <AudioMutedOutlined />}
                onClick={toggleAudio}
              />
              <Button
                type={screenShare ? 'primary' : 'default'}
                danger={screenShare}
                shape="circle"
                size="large"
                icon={<StopOutlined />}
                onClick={toggleScreenShare}
              />
            </Space>
          </div>
        </div>
      </Layout>
    </Layout>
  )
}
