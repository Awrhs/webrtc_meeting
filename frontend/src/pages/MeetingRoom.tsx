import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useDispatch, useSelector } from 'react-redux'
import { Layout, Button, Space, Tag, message, Modal, Dropdown, MenuProps, Tabs } from 'antd'
import {
  VideoCameraOutlined,
  VideoCameraFilled,
  VideoCameraAddOutlined,
  AudioOutlined,
  AudioMutedOutlined,
  StopOutlined,
  LockOutlined,
  UnlockOutlined,
  DeleteOutlined,
  LogoutOutlined,
  FileSearchOutlined,
} from '@ant-design/icons'
import { RootState, AppDispatch } from '@/store'
import { joinRoom, getRoomMessages, refreshMembers, addMessage, setConnected, clearCurrentRoom, updateMember, removeMember, lockRoom, deleteRoom, getAuditLogs } from '@/store/slices/roomSlice'
import { MediaE2EEState, MediaE2EEManager } from '@/utils/mediaE2EE'
import { api } from '@/utils/api'
import VideoGrid, { RemoteStreamMeta } from '@/components/VideoGrid'
import Chat from '@/components/Chat'
import MemberList from '@/components/MemberList'
import RecordingsModal from '@/components/RecordingsModal'
import { JoinRoomResponse, RoomMember, AuditLog } from '@/types'
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
  // SFU 媒体连接是否已建立（头部持久显示，比瞬时 toast 更可靠）
  const [mediaConnected, setMediaConnected] = useState(false)
  // 远端流元信息（streamId -> kind/rid），用于区分摄像头流与屏幕共享流
  const [remoteStreamMeta, setRemoteStreamMeta] = useState<Map<string, RemoteStreamMeta>>(new Map())
  // 由 useLocalMedia 提供：注入入会时的麦克风流，供开启摄像头时复用
  const initialStreamSetterRef = useRef<((s: MediaStream) => void) | null>(null)
  // 私聊目标（null = 群聊视图）
  const [privateTarget, setPrivateTarget] = useState<RoomMember | null>(null)
  // 审计日志弹窗
  const [auditLogsVisible, setAuditLogsVisible] = useState(false)
  // 会议录制弹窗（房主/管理员）
  const [recordingsVisible, setRecordingsVisible] = useState(false)
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([])
  const [auditLogsLoading, setAuditLogsLoading] = useState(false)

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
      // 记录流元信息（kind/rid）：VideoGrid 由此区分摄像头流与屏幕共享流
      const meta = sfuConnectionRef.current?.getStreamMeta(stream.id)
      if (meta) {
        setRemoteStreamMeta(prev => {
          if (prev.get(stream.id) === meta) return prev
          const next = new Map(prev)
          next.set(stream.id, meta)
          return next
        })
      }
      addRemoteStream(userId, stream)
    },
    mediaE2EEManagerRef,
    localStreamRef,
    onInitialStream: (s) => initialStreamSetterRef.current?.(s),
    onConnectionChange: setMediaConnected,
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
    cameraDevices,
    activeCameraId,
    refreshCameraDevices,
    switchCamera,
    setInitialStream,
  } = useLocalMedia({ sfuConnectionRef, currentRoomId: currentRoom?.id })

  useEffect(() => {
    initialStreamSetterRef.current = setInitialStream
  }, [setInitialStream])

  const signalingDisconnectRef = useRef<(() => void) | null>(null)
  // 稳定引用：入会 effect 只依赖 [id, dispatch]，避免 currentRoom 更新
  // 触发 effect 重建 → 清理函数离会 → 再次入会的无限循环
  const notifyServerLeaveRef = useRef<() => void>(() => {})

  // 服务端记录离开：显式离开按钮 + 页面卸载（keepalive 请求会存活到页面关闭）。
  // 断线未重连的用户由 Hub 宽限期机制自动补记。
  const notifyServerLeave = useCallback(() => {
    notifyServerLeaveRef.current()
  }, [])

  notifyServerLeaveRef.current = () => {
    const roomId = currentRoom?.id || (id ? parseInt(id) : 0)
    if (!roomId) return
    const token = localStorage.getItem('token')
    if (!token) return
    try {
      void fetch(`${api.defaults.baseURL}/api/v1/rooms/${roomId}/leave`, {
        method: 'POST',
        keepalive: true,
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
        },
      })
    } catch {
      // 忽略：宽限期机制会兑底
    }
  }

  useEffect(() => {
    const onHide = () => {
      notifyServerLeave()
    }
    window.addEventListener('pagehide', onHide)
    return () => {
      window.removeEventListener('pagehide', onHide)
    }
  }, [notifyServerLeave])

  const leaveMeeting = useCallback(() => {
    cleanupLocalMedia()
    sfuDisconnect()
    signalingDisconnectRef.current?.()
    clearRemoteStreams()
    setRemoteStreamMeta(new Map())
    setPrivateTarget(null)
    setMediaE2EEState('disabled')
    setMediaConnected(false)
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
      refreshMembers: async () => {
        try {
          const result = await dispatch(refreshMembers(parseInt(id!))).unwrap()
          return result.members
        } catch {
          return undefined
        }
      },
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
      notifyServerLeave()
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
      joinResult.sfu_url,
    )
  }

  const handleLeave = () => {
    Modal.confirm({
      title: '确认离开会议',
      content: '你确定要离开本次会议吗？',
      onOk: () => {
        notifyServerLeave()
        leaveMeeting()
        navigate('/')
      },
    })
  }

  const handleShowAuditLogs = async () => {
    if (!currentRoom) return
    setAuditLogsVisible(true)
    setAuditLogsLoading(true)
    try {
      const result = await dispatch(getAuditLogs({ roomId: currentRoom.id, limit: 50 })).unwrap()
      setAuditLogs(result)
    } catch (error: any) {
      message.error(error || '获取审计日志失败')
    } finally {
      setAuditLogsLoading(false)
    }
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

    // owner/admin 可查看审计日志
    const currentMember = members.find(m => m.user_id === currentUserId)
    if (currentMember && (currentMember.role === 'owner' || currentMember.role === 'admin')) {
      items.push({
        key: 'audit',
        icon: <FileSearchOutlined />,
        label: '审计日志',
        onClick: handleShowAuditLogs,
      })
      items.push({
        key: 'recordings',
        icon: <VideoCameraOutlined />,
        label: '会议录制',
        onClick: () => setRecordingsVisible(true),
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

  // 摄像头设备切换菜单
  const cameraMenuItems: MenuProps['items'] = [
    ...cameraDevices.map(d => ({
      key: d.deviceId,
      label: d.label || `摄像头 ${d.deviceId.slice(0, 8)}`,
      disabled: d.deviceId === activeCameraId,
    })),
    { type: 'divider' as const },
    { key: '__refresh', label: '刷新设备列表' },
  ]

  const auditActionText: Record<string, string> = {
    kick: '踢出成员',
    mute: '禁言',
    unmute: '解除禁言',
    set_admin: '设置管理员',
    remove_admin: '撤销管理员',
    transfer_owner: '转让房主',
    lock_room: '锁定房间',
    unlock_room: '解锁房间',
    delete_room: '解散房间',
  }

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

  const auditModal = (
    <Modal
      title="房间审计日志"
      open={auditLogsVisible}
      onCancel={() => setAuditLogsVisible(false)}
      footer={[
        <Button key="close" onClick={() => setAuditLogsVisible(false)}>关闭</Button>,
      ]}
      width={720}
    >
      {auditLogsLoading ? (
        <div style={{ textAlign: 'center', padding: 24 }}>加载中...</div>
      ) : auditLogs.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 24, color: '#999' }}>暂无审计记录</div>
      ) : (
        <div style={{ maxHeight: 400, overflowY: 'auto' }}>
          {auditLogs.map((log: AuditLog) => (
            <div key={log.id} style={{ padding: '6px 0', borderBottom: '1px solid #f0f0f0', fontSize: 13 }}>
              <span style={{ color: '#888' }}>{new Date(log.created_at).toLocaleString()}</span>
              {' '}
              <Tag>{auditActionText[log.action] || log.action}</Tag>
              <span>操作人 #{log.user_id}</span>
              {log.target_id && <span> → 对象 #{log.target_id}</span>}
              {log.ip_address && <span style={{ color: '#bbb' }}> ({log.ip_address})</span>}
            </div>
          ))}
        </div>
      )}
    </Modal>
  )

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
          <Tag data-testid="media-status" color={mediaConnected ? 'green' : 'default'}>
            {mediaConnected ? '媒体已连接' : '媒体连接中'}
          </Tag>
          <span style={{ marginLeft: 16, color: '#888' }}>
            {currentRoom?.current_size}/{currentRoom?.max_members} 人
          </span>
        </div>
        <Dropdown menu={{ items }} placement="bottomRight">
          <Button type="primary">房间操作</Button>
        </Dropdown>
      </Header>
      <Layout className="meeting-main-layout">
        <Content style={{ background: '#000', overflow: 'hidden' }}>
          <VideoGrid
            members={members}
            currentUserId={currentUserId}
            localStream={getMyRemoteStream()}
            screenStream={screenShare ? screenStream : null}
            remoteStreams={remoteStreams}
            remoteStreamMeta={remoteStreamMeta}
          />
        </Content>
        <div
          className="meeting-side-panel"
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
              { key: 'members', label: '成员列表', children: (
                <MemberList
                  roomId={parseInt(id!)}
                  members={members}
                  currentUserId={currentUserId}
                  permissions={myPermissions}
                  onStartPrivateChat={(member) => setPrivateTarget(member)}
                  onSetVideoQuality={(userId, quality) => sfuConnectionRef.current?.setVideoQuality(userId, quality)}
                />
              ) },
              { key: 'chat', label: privateTarget ? `私聊：${privateTarget.nickname}` : '聊天', children: (
                <Chat
                  roomId={parseInt(id!)}
                  messages={messages}
                  signaling={signaling.signalingRef.current}
                  privateTarget={privateTarget}
                  onClosePrivate={() => setPrivateTarget(null)}
                />
              ) },
            ]}
          />
          <div className="meeting-control-bar" style={{ padding: 16, borderTop: '1px solid #f0f0f0' }}>
            <Space size="middle" style={{ width: '100%', justifyContent: 'center' }}>
              <Button
                type={videoOn ? 'primary' : 'default'}
                shape="circle"
                size="large"
                icon={videoOn ? <VideoCameraFilled /> : <VideoCameraOutlined />}
                onClick={toggleVideo}
                title={videoOn ? '关闭摄像头' : '开启摄像头'}
                aria-label={videoOn ? '关闭摄像头' : '开启摄像头'}
              />
              <Dropdown
                menu={{
                  items: cameraMenuItems,
                  onClick: ({ key }) => {
                    if (key === '__refresh') {
                      void refreshCameraDevices()
                    } else {
                      void switchCamera(key)
                    }
                  },
                }}
                trigger={['click']}
                onOpenChange={(open) => {
                  if (open) void refreshCameraDevices()
                }}
              >
                <Button shape="circle" size="large" icon={<VideoCameraAddOutlined />} title="切换摄像头" />
              </Dropdown>
              <Button
                type={audioOn ? 'primary' : 'default'}
                danger={!audioOn}
                shape="circle"
                size="large"
                icon={audioOn ? <AudioOutlined /> : <AudioMutedOutlined />}
                onClick={toggleAudio}
                title={audioOn ? '关闭麦克风' : '开启麦克风'}
                aria-label={audioOn ? '关闭麦克风' : '开启麦克风'}
              />
              <Button
                type={screenShare ? 'primary' : 'default'}
                danger={screenShare}
                shape="circle"
                size="large"
                icon={<StopOutlined />}
                onClick={toggleScreenShare}
                title={screenShare ? '停止共享屏幕' : '共享屏幕'}
                aria-label={screenShare ? '停止共享屏幕' : '共享屏幕'}
              />
            </Space>
          </div>
        </div>
      </Layout>
      {auditModal}
      <RecordingsModal
        roomId={parseInt(id!)}
        open={recordingsVisible}
        onClose={() => setRecordingsVisible(false)}
        mediaE2EEEnabled={mediaE2EEState === 'enabled'}
      />
    </Layout>
  )
}
