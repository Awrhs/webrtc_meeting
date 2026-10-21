import { Avatar, Dropdown, MenuProps, message, Modal, Tag } from 'antd'
import { UserOutlined, MoreOutlined, AudioMutedOutlined, MessageOutlined, DashboardOutlined } from '@ant-design/icons'
import { RoomMember, Permission } from '@/types'
import { useNavigate } from 'react-router-dom'
import { useDispatch } from 'react-redux'
import { useEffect, useState } from 'react'
import { api } from '@/utils/api'
import { AppDispatch } from '@/store'
import { kickMember, muteMember, setAdmin, transferOwner } from '@/store/slices/roomSlice'

interface MemberListProps {
  roomId: number
  members: RoomMember[]
  currentUserId: number
  permissions: Permission[]
  onStartPrivateChat: (member: RoomMember) => void
  onSetVideoQuality: (userId: number, quality: 'auto' | 'high' | 'low') => void
}

export default function MemberList({
  roomId,
  members,
  currentUserId,
  permissions,
  onStartPrivateChat,
  onSetVideoQuality,
}: MemberListProps) {
  const navigate = useNavigate()
  const dispatch = useDispatch<AppDispatch>()
  // 实时在线状态：房间内当前 WebSocket 在线的用户（每 10s 轮询 presence 接口）
  const [onlineUserIds, setOnlineUserIds] = useState<Set<number>>(new Set())
  // 每个成员的视频质量选择（本地展示状态）
  const [qualityByUser, setQualityByUser] = useState<Map<number, 'auto' | 'high' | 'low'>>(new Map())

  useEffect(() => {
    let stopped = false
    const refresh = async () => {
      try {
        const response = await api.get(`/api/v1/rooms/${roomId}/presence`)
        if (!stopped && Array.isArray(response.data?.online_user_ids)) {
          setOnlineUserIds(new Set(response.data.online_user_ids as number[]))
        }
      } catch {
        // 轮询失败静默忽略
      }
    }
    void refresh()
    const timer = setInterval(refresh, 10000)
    return () => {
      stopped = true
      clearInterval(timer)
    }
  }, [roomId])

  const canKick = permissions.includes('kick_member')
  const canMute = permissions.includes('mute_member')
  const canSetAdmin = permissions.includes('set_admin')
  const canTransferOwner = permissions.includes('transfer_owner')
  const currentMember = members.find(m => m.user_id === currentUserId)
  const isOwner = currentMember?.role === 'owner'

  const handleKick = async (member: RoomMember) => {
    Modal.confirm({
      title: '确认踢人',
      content: `确定要踢出 ${member.nickname} 吗？被踢用户10分钟内无法重新加入。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await dispatch(kickMember({ roomId, userId: member.user_id })).unwrap()
          message.success('已踢出')
        } catch (error: any) {
          message.error(error || '操作失败')
        }
      },
    })
  }

  const handleMute = async (member: RoomMember, muted: boolean) => {
    Modal.confirm({
      title: muted ? '确认禁言' : '解除禁言',
      content: `${muted ? '禁言后' : '解除禁言后'} ${member.nickname} ${muted ? '无法发送消息' : '可以发送消息'}。`,
      onOk: async () => {
        try {
          await dispatch(muteMember({ roomId, userId: member.user_id, muted })).unwrap()
          message.success(muted ? '已禁言' : '已解除禁言')
        } catch (error: any) {
          message.error(error || '操作失败')
        }
      },
    })
  }

  const handleSetAdmin = async (member: RoomMember, makeAdmin: boolean) => {
    Modal.confirm({
      title: makeAdmin ? '设置管理员' : '撤销管理员',
      content: `${makeAdmin ? '设置' : '撤销'} ${member.nickname} ${makeAdmin ? '为管理员' : '管理员权限'}。`,
      onOk: async () => {
        try {
          await dispatch(setAdmin({ roomId, userId: member.user_id, isAdmin: makeAdmin })).unwrap()
          message.success('操作成功')
        } catch (error: any) {
          message.error(error || '操作失败')
        }
      },
    })
  }

  const handleTransferOwner = async (member: RoomMember) => {
    Modal.confirm({
      title: '确认转让房主',
      content: `确定要将房主转让给 ${member.nickname} 吗？转让后你将变为普通成员。`,
      onOk: async () => {
        try {
          await dispatch(transferOwner({ roomId, userId: member.user_id })).unwrap()
          message.success('转让成功')
          navigate('/')
        } catch (error: any) {
          message.error(error || '操作失败')
        }
      },
    })
  }

  const handleQuality = (member: RoomMember, quality: 'auto' | 'high' | 'low') => {
    setQualityByUser(prev => {
      const next = new Map(prev)
      next.set(member.user_id, quality)
      return next
    })
    onSetVideoQuality(member.user_id, quality)
  }

  const getMenuItems = (member: RoomMember): MenuProps['items'] => {
    const items: MenuProps['items'] = []

    // 房主不能被操作
    if (member.role === 'owner') return items
    // 不能操作自己
    if (member.user_id === currentUserId) return items

    // 管理员只能操作普通成员
    if (!isOwner && member.role === 'admin') return items

    if (canMute) {
      if (!member.is_muted) {
        items.push({
          key: 'mute',
          label: '禁言',
          danger: true,
          onClick: () => handleMute(member, true),
        })
      } else {
        items.push({
          key: 'unmute',
          label: '解除禁言',
          onClick: () => handleMute(member, false),
        })
      }
    }

    if (canSetAdmin) {
      if (member.role !== 'admin') {
        items.push({
          key: 'set-admin',
          label: '设置管理员',
          onClick: () => handleSetAdmin(member, true),
        })
      } else {
        items.push({
          key: 'remove-admin',
          label: '撤销管理员',
          onClick: () => handleSetAdmin(member, false),
        })
      }
    }

    if (canTransferOwner) {
      items.push({
        key: 'transfer',
        label: '转让房主',
        danger: true,
        onClick: () => handleTransferOwner(member),
      })
    }

    if (canKick) {
      if (items.length > 0) {
        items.push({ type: 'divider' })
      }
      items.push({
        key: 'kick',
        label: '踢出房间',
        danger: true,
        onClick: () => handleKick(member),
      })
    }

    return items
  }

  const hasPermissionOnMember = (member: RoomMember) => {
    if (member.role === 'owner') return false
    if (member.user_id === currentUserId) return false
    if (!isOwner && member.role === 'admin') return false
    return canKick || canMute || canSetAdmin || canTransferOwner
  }

  return (
    <div style={{ maxHeight: 500, overflowY: 'auto' }}>
      {members.map(member => {
        const isOnline = onlineUserIds.has(member.user_id) || member.user_id === currentUserId
        const quality = qualityByUser.get(member.user_id) || 'auto'
        const qualityMenu: MenuProps['items'] = [
          { key: 'auto', label: '自动（自适应）', disabled: quality === 'auto' },
          { key: 'high', label: '高清（high 层）', disabled: quality === 'high' },
          { key: 'low', label: '流畅（low 层）', disabled: quality === 'low' },
        ]
        return (
          <div key={member.user_id} className="member-item">
            <Avatar size="default" src={member.avatar} icon={<UserOutlined />} />
            <div className="member-info">
              <div className="member-name">
                <span
                  title={isOnline ? '在线' : '离线'}
                  style={{
                    display: 'inline-block',
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    marginRight: 6,
                    background: isOnline ? '#52c41a' : '#d9d9d9',
                  }}
                />
                <span>{member.nickname}</span>
                <span className={`role-badge role-${member.role}`}>
                  {member.role === 'owner' ? '房主' : member.role === 'admin' ? '管理员' : '成员'}
                </span>
                {!member.is_audio_on && <AudioMutedOutlined style={{ color: '#f5222d', fontSize: 14 }} />}
              </div>
            </div>
            {member.user_id !== currentUserId && (
              <>
                <Dropdown
                  menu={{ items: qualityMenu, onClick: ({ key }) => handleQuality(member, key as 'auto' | 'high' | 'low') }}
                  trigger={['click']}
                >
                  <DashboardOutlined
                    style={{ cursor: 'pointer', marginRight: 8, color: quality === 'auto' ? '#999' : '#1677ff' }}
                    title={`视频质量：${quality === 'auto' ? '自动' : quality}`}
                  />
                </Dropdown>
                <MessageOutlined
                  style={{ cursor: 'pointer', marginRight: 8 }}
                  title={`私聊 ${member.nickname}`}
                  onClick={() => onStartPrivateChat(member)}
                />
                {hasPermissionOnMember(member) && (
                  <Dropdown menu={{ items: getMenuItems(member) }} placement="bottomRight">
                    <MoreOutlined style={{ cursor: 'pointer' }} />
                  </Dropdown>
                )}
              </>
            )}
            {member.user_id === currentUserId && (
              <Tag color="blue" style={{ marginLeft: 'auto' }}>我</Tag>
            )}
          </div>
        )
      })}
    </div>
  )
}
