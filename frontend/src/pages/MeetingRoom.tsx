import { useState, useEffect, useRef } from 'react'
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
import { joinRoom, getRoomMessages, addMessage, setConnected, clearCurrentRoom, updateMember, removeMember, kickMember, muteMember, setAdmin, transferOwner, lockRoom, deleteRoom } from '@/store/slices/roomSlice'
import { api } from '@/utils/api'
import { SignalingClient } from '@/utils/signaling'
import { getMediaStream, SFUConnection } from '@/utils/webrtc'
import VideoGrid from '@/components/VideoGrid'
import Chat from '@/components/Chat'
import MemberList from '@/components/MemberList'

const { Header, Content } = Layout

export default function MeetingRoom() {
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const dispatch = useDispatch<AppDispatch>()
  const { currentRoom, members, messages, myPermissions } = useSelector((state: RootState) => state.room)
  const { user } = useSelector((state: RootState) => state.auth)

  const [videoOn, setVideoOn] = useState(false)
  const [audioOn, setAudioOn] = useState(true)
  const [screenShare, setScreenShare] = useState(false)
  const [loading, setLoading] = useState(false)
  const [localStream, setLocalStream] = useState<MediaStream | null>(null)
  const [screenStream, setScreenStream] = useState<MediaStream | null>(null)

  const signalingRef = useRef<SignalingClient | null>(null)
  const sfuConnectionRef = useRef<SFUConnection | null>(null)
  const localStreamRef = useRef<MediaStream | null>(null)
  const screenStreamRef = useRef<MediaStream | null>(null)
  // 每个 user_id 对应一个 MediaStream 数组（可能有多个音视频轨道）
  const remoteStreamsRef = useRef<Map<number, MediaStream[]>>(new Map())
  // 强制刷新渲染的 trigger
  const [, forceUpdate] = useState({})

  const currentUserId = user?.id || 0
  const currentMember = members.find(m => m.user_id === currentUserId)

  // 基于权限的功能检查
  const canKick = myPermissions.includes('kick_member')
  const canMute = myPermissions.includes('mute_member')
  const canSetAdmin = myPermissions.includes('set_admin')
  const canLockRoom = myPermissions.includes('lock_room')
  const canDeleteRoom = myPermissions.includes('delete_room')
  const canTransferOwner = myPermissions.includes('transfer_owner')
  const isOwner = currentMember?.role === 'owner'
  const isAdmin = currentMember?.role === 'admin' || isOwner

  useEffect(() => {
    const verifyCode = searchParams.get('verify') || ''
    const roomId = parseInt(id!)

    setLoading(true)
    dispatch(joinRoom({ id: id!, data: { verify_code: verifyCode } }))
      .unwrap()
      .then(async () => {
        dispatch(getRoomMessages(roomId))
        await joinMeeting()
      })
      .catch((error) => {
        message.error(error)
        navigate('/')
      })
      .finally(() => {
        setLoading(false)
      })

    return () => {
      leaveMeeting()
      dispatch(clearCurrentRoom())
    }
  }, [id, dispatch])

  const joinMeeting = async () => {
    const token = localStorage.getItem('token')
    if (!token) return

    const signaling = new SignalingClient()
    signalingRef.current = signaling

    signaling.connect().then(() => {
      dispatch(setConnected(true))

      signaling.on('user_joined', (msg) => {
        message.info(`${msg.data?.user_name || '用户'}加入了房间`)
      })

      signaling.on('user_left', (msg) => {
        dispatch(removeMember(msg.user_id))
        remoteStreamsRef.current.delete(msg.user_id)
        message.info(`${msg.data?.user_name || '用户'}离开了房间`)
      })

      signaling.on('chat_message', (msg) => {
        dispatch(addMessage(msg.data))
      })

      signaling.on('kicked', () => {
        message.error('你已被踢出房间')
        leaveMeeting()
        navigate('/')
      })

      signaling.on('muted', (msg) => {
        dispatch(updateMember({
          ...members.find(m => m.user_id === msg.user_id)!,
          is_muted: msg.data.is_muted,
        }))
        if (msg.user_id === currentUserId && msg.data.is_muted) {
          message.info('你已被禁言')
        }
      })

      signaling.on('role_changed', (msg) => {
        dispatch(updateMember({
          ...members.find(m => m.user_id === msg.user_id)!,
          role: msg.data.role,
        }))
      })

      // 监听其他人的媒体状态变更
      signaling.on('media_state', (msg) => {
        if (msg.user_id !== currentUserId) {
          const member = members.find(m => m.user_id === msg.user_id)
          if (member) {
            dispatch(updateMember({
              ...member,
              is_audio_on: msg.data.is_audio_on,
              is_video_on: msg.data.is_video_on,
            }))
          }
        }
      })

      signaling.joinRoom(parseInt(id!), currentUserId)
    })

    try {
      const stream = await getMediaStream(false, true)
      localStreamRef.current = stream
      setLocalStream(stream)
      localStreamRef.current.getVideoTracks().forEach(track => {
        track.enabled = false
      })
      localStreamRef.current.getAudioTracks().forEach(track => {
        track.enabled = true
      })

      // 使用新的 SFUConnection 类
      const sfuConnection = new SFUConnection(
        parseInt(id!),
        currentUserId,
        // 处理远程媒体流回调
        (userId: number, track: MediaStreamTrack, stream: MediaStream) => {
          console.log('[Meeting] Received remote track:', track.kind, 'from user', userId)
          const existing = remoteStreamsRef.current.get(userId) || []
          if (!existing.includes(stream)) {
            existing.push(stream)
            remoteStreamsRef.current.set(userId, existing)
            forceUpdate({})
          }
        },
        // 连接成功回调
        () => {
          console.log('[Meeting] SFU connection established successfully')
          message.success('媒体连接已建立')
        },
        // 连接断开回调
        () => {
          console.log('[Meeting] SFU connection disconnected')
        },
        // 错误回调
        (error: string) => {
          console.error('[Meeting] SFU error:', error)
          message.error(error)
        }
      )

      sfuConnectionRef.current = sfuConnection
      await sfuConnection.connect(stream)

    } catch (error) {
      console.error('Failed to get media stream', error)
      message.error('获取摄像头/麦克风权限失败')
    }
  }

  const leaveMeeting = () => {
    if (screenStreamRef.current) {
      screenStreamRef.current.getTracks().forEach(track => track.stop())
      screenStreamRef.current = null
    }
    if (localStreamRef.current) {
      localStreamRef.current.getTracks().forEach(track => track.stop())
      localStreamRef.current = null
    }
    // 使用新的 SFUConnection.disconnect()
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.disconnect()
      sfuConnectionRef.current = null
    }
    if (signalingRef.current) {
      signalingRef.current.disconnect()
      signalingRef.current = null
    }
    remoteStreamsRef.current.clear()
    setLocalStream(null)
    setVideoOn(false)
    setAudioOn(false)
    setScreenShare(false)
    forceUpdate({})
  }

  const toggleVideo = async () => {
    if (!localStreamRef.current) {
      try {
        console.log('[Meeting] Requesting camera permission...')
        const stream = await getMediaStream(true, audioOn)
        console.log('[Meeting] Got camera stream:', stream.getVideoTracks().length, 'tracks')
        localStreamRef.current = stream
        setLocalStream(stream)
        const videoTrack = stream.getVideoTracks()[0]
        // 使用 SFUConnection.replaceVideoTrack 更新视频轨道
        if (sfuConnectionRef.current) {
          await sfuConnectionRef.current.replaceVideoTrack(videoTrack)
        }
      } catch (error) {
        console.error('[Meeting] Camera error:', error)
        message.error('无法开启摄像头：' + (error as Error).message)
        return
      }
    } else {
      // 直接使用 SFUConnection.muteVideo 切换
      if (sfuConnectionRef.current) {
        sfuConnectionRef.current.muteVideo(videoOn)
      }
    }
    setVideoOn(!videoOn)

    if (signalingRef.current && currentRoom) {
      signalingRef.current.send({
        type: 'media_state',
        room_id: currentRoom.id,
        user_id: currentUserId,
        data: { video_on: !videoOn, audio_on: audioOn },
      })
    }

    if (currentRoom) {
      api.put(`/api/v1/rooms/${currentRoom.id}/media-state`, { video_on: !videoOn })
    }
  }

  const toggleAudio = () => {
    if (!localStreamRef.current) return
    // 使用 SFUConnection.muteAudio
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.muteAudio(audioOn)
    }
    setAudioOn(!audioOn)

    if (signalingRef.current && currentRoom) {
      signalingRef.current.send({
        type: 'media_state',
        room_id: currentRoom.id,
        user_id: currentUserId,
        data: { video_on: videoOn, audio_on: !audioOn },
      })
    }
  }

  const toggleScreenShare = async () => {
    if (!screenShare) {
      try {
        const screenStream = await navigator.mediaDevices.getDisplayMedia({
          video: true,
          audio: false,
        })
        screenStreamRef.current = screenStream
        setScreenStream(screenStream)
        const videoTrack = screenStream.getVideoTracks()[0]
        videoTrack.onended = () => {
          stopScreenShare()
        }

        // 使用 SFUConnection.addVideoTrack 添加屏幕共享轨道
        // 不替换摄像头轨道，实现同时显示
        if (sfuConnectionRef.current) {
          await sfuConnectionRef.current.addVideoTrack(videoTrack, screenStream)
        }
        setScreenShare(true)
        message.success('屏幕共享已开启')
      } catch (error) {
        console.error('Screen share error:', error)
        message.error('无法开启屏幕共享')
      }
    } else {
      stopScreenShare()
    }
  }

  const stopScreenShare = () => {
    if (screenStreamRef.current) {
      screenStreamRef.current.getTracks().forEach(track => track.stop())
      screenStreamRef.current = null
    }

    // 移除屏幕共享轨道
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.removeScreenTrack()
    }

    setScreenStream(null)
    setScreenShare(false)
    message.info('屏幕共享已关闭')
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

  // 基于权限动态生成菜单
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
            remoteStreams={remoteStreamsRef.current}
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
              { key: 'chat', label: '聊天', children: <Chat roomId={parseInt(id!)} messages={messages} signaling={signalingRef.current} /> },
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
