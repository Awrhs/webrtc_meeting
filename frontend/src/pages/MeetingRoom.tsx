import { useState, useEffect, useRef } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Button, Card, List, Space, Tag, message, Modal, Dropdown, MenuProps, Tabs, Input } from 'antd'
import {
  VideoCameraOutlined,
  VideoCameraFilled,
  AudioOutlined,
  AudioMutedOutlined,
  ScreenShareOutlined,
  StopOutlined,
  LockOutlined,
  UnlockOutlined,
  CloseOutlined,
  UserOutlined,
  MessageOutlined,
  StarOutlined,
  DeleteOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { RootState, AppDispatch } from '@/store'
import { joinRoom, getRoomMessages, addMessage, setConnected, clearCurrentRoom, updateMember, removeMember } from '@/store/slices/roomSlice'
import { api } from '@/utils/api'
import { SignalingClient } from '@/utils/signaling'
import { getMediaStream, connectToSFU } from '@/utils/webrtc'
import { Message, RoomMember } from '@/types'
import VideoGrid from '@/components/VideoGrid'
import Chat from '@/components/Chat'
import MemberList from '@/components/MemberList'

const { Header, Content } = Layout

export default function MeetingRoom() {
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const dispatch = useDispatch<AppDispatch>()
  const { currentRoom, members, messages, connected } = useSelector((state: RootState) => state.room)
  const { user } = useSelector((state: RootState) => state.auth)

  const [videoOn, setVideoOn] = useState(false)
  const [audioOn, setAudioOn] = useState(true)
  const [screenShare, setScreenShare] = useState(false)
  const [isJoined, setIsJoined] = useState(false)
  const [loading, setLoading] = useState(false)

  const signalingRef = useRef<SignalingClient | null>(null)
  const peerConnectionRef = useRef<RTCPeerConnection | null>(null)
  const localStreamRef = useRef<MediaStream | null>(null)
  const remoteStreamsRef = useRef<Map<number, MediaStream>>(new Map())

  const currentUserId = user?.id || 0
  const currentMember = members.find(m => m.user_id === currentUserId)
  const isOwner = currentMember?.role === 'owner'
  const isAdmin = currentMember?.role === 'admin' || isOwner

  useEffect(() => {
    const verifyCode = searchParams.get('verify') || ''
    const roomId = parseInt(id!)

    setLoading(true)
    dispatch(joinRoom({ id: id!, data: { room_id: currentRoom?.room_id, verify_code: verifyCode } }))
      .unwrap()
      .then(async () => {
        dispatch(getRoomMessages(roomId))
        await joinMeeting()
        setIsJoined(true)
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

      signaling.send({
        type: 'join_room',
        room_id: parseInt(id!),
        user_id: currentUserId,
        data: null,
      })
    })

    try {
      const stream = await getMediaStream(false, true)
      localStreamRef.current = stream
      localStreamRef.current.getVideoTracks().forEach(track => {
        track.enabled = false
      })
      localStreamRef.current.getAudioTracks().forEach(track => {
        track.enabled = true
      })

      const pc = await connectToSFU(parseInt(id!), currentUserId, stream)
      peerConnectionRef.current = pc

      pc.ontrack = (event) => {
        event.streams.forEach(stream => {
          remoteStreamsRef.current.set(0, stream)
        })
      }
    } catch (error) {
      console.error('Failed to get media stream', error)
      message.error('获取摄像头/麦克风权限失败')
    }
  }

  const leaveMeeting = () => {
    if (localStreamRef.current) {
      localStreamRef.current.getTracks().forEach(track => track.stop())
      localStreamRef.current = null
    }
    if (peerConnectionRef.current) {
      peerConnectionRef.current.close()
      peerConnectionRef.current = null
    }
    if (signalingRef.current) {
      signalingRef.current.disconnect()
      signalingRef.current = null
    }
    remoteStreamsRef.current.clear()
  }

  const toggleVideo = async () => {
    if (!localStreamRef.current) {
      try {
        const stream = await getMediaStream(true, audioOn)
        localStreamRef.current = stream
        const videoTrack = stream.getVideoTracks()[0]
        if (peerConnectionRef.current) {
          const sender = peerConnectionRef.current.getSenders().find(s => s.track?.kind === 'video')
          if (sender) {
            sender.replaceTrack(videoTrack)
          } else {
            peerConnectionRef.current.addTrack(videoTrack, stream)
          }
        }
      } catch (error) {
        message.error('无法开启摄像头')
        return
      }
    } else {
      const videoTracks = localStreamRef.current.getVideoTracks()
      if (videoTracks.length > 0) {
        videoTracks[0].enabled = !videoOn
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
    const audioTracks = localStreamRef.current.getAudioTracks()
    if (audioTracks.length > 0) {
      audioTracks[0].enabled = !audioOn
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
          audio: true,
        })
        const videoTrack = screenStream.getVideoTracks()[0]
        videoTrack.onended = () => {
          setScreenShare(false)
        }

        if (peerConnectionRef.current && localStreamRef.current) {
          const sender = peerConnectionRef.current.getSenders().find(s => s.track?.kind === 'video')
          if (sender) {
            sender.replaceTrack(videoTrack)
          }
        }
        setScreenShare(true)
      } catch (error) {
        message.error('无法开启屏幕共享')
      }
    } else {
      if (localStreamRef.current && peerConnectionRef.current) {
        const videoTrack = localStreamRef.current.getVideoTracks()[0]
        const sender = peerConnectionRef.current.getSenders().find(s => s.track?.kind === 'video')
        if (sender && videoTrack) {
          sender.replaceTrack(videoTrack)
        }
      }
      setScreenShare(false)
    }
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
      await api.post(`/api/v1/rooms/${currentRoom.id}/lock`)
      message.success(currentRoom.is_locked ? '房间已解锁' : '房间已锁定')
      window.location.reload()
    } catch (error: any) {
      message.error(error.response?.data?.error || '操作失败')
    }
  }

  const handleDeleteRoom = () => {
    if (!currentRoom) return
    Modal.confirm({
      title: '确认解散房间',
      content: '解散后所有成员将被移出，且无法恢复，确定解散吗？',
      danger: true,
      onOk: async () => {
        try {
          await api.delete(`/api/v1/rooms/${currentRoom.id}`)
          message.success('房间已解散')
          navigate('/')
        } catch (error: any) {
          message.error(error.response?.data?.error || '操作失败')
        }
      },
    })
  }

  const getMyRemoteStream = () => {
    if (videoOn && localStreamRef.current) {
      return localStreamRef.current
    }
    return null
  }

  const items: MenuProps['items'] = isOwner ? [
    {
      key: 'lock',
      icon: currentRoom?.is_locked ? <UnlockOutlined /> : <LockOutlined />,
      label: currentRoom?.is_locked ? '解锁房间' : '锁定房间',
      onClick: handleLockRoom,
    },
    {
      key: 'delete',
      icon: <DeleteOutlined />,
      label: '解散房间',
      danger: true,
      onClick: handleDeleteRoom,
    },
    {
      type: 'divider',
    },
    {
      key: 'leave',
      icon: <LogoutOutlined />,
      label: '离开会议',
      onClick: handleLeave,
    },
  ] : [
    {
      key: 'leave',
      icon: <LogoutOutlined />,
      label: '离开会议',
      onClick: handleLeave,
    },
  ]

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
              { key: 'members', label: '成员列表', children: <MemberList roomId={parseInt(id!)} members={members} isAdmin={isAdmin} isOwner={isOwner} /> },
              { key: 'chat', label: '聊天', children: <Chat roomId={parseInt(id!)} messages={messages} /> },
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
                icon={screenShare ? <StopOutlined /> : <ScreenShareOutlined />}
                onClick={toggleScreenShare}
              />
            </Space>
          </div>
        </div>
      </Layout>
    </Layout>
  )
}
