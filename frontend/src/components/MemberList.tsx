import { useState } from 'react'
import { Avatar, Dropdown, MenuProps, message, Modal } from 'antd'
import { UserOutlined, MoreOutlined } from '@ant-design/icons'
import { RoomMember } from '@/types'
import { api } from '@/utils/api'
import { useNavigate } from 'react-router-dom'

interface MemberListProps {
  roomId: number
  members: RoomMember[]
  isAdmin: boolean
  isOwner: boolean
}

export default function MemberList({ roomId, members, isAdmin, isOwner }: MemberListProps) {
  const navigate = useNavigate()

  const handleKick = (member: RoomMember) => {
    Modal.confirm({
      title: '确认踢人',
      content: `确定要踢出 ${member.nickname} 吗？被踢用户10分钟内无法重新加入。`,
      danger: true,
      onOk: async () => {
        try {
          await api.post(`/api/v1/rooms/${roomId}/members/${member.user_id}/kick`)
          message.success('已踢出')
          window.location.reload()
        } catch (error: any) {
          message.error(error.response?.data?.error || '操作失败')
        }
      },
    })
  }

  const handleMute = (member: RoomMember, muted: boolean) => {
    Modal.confirm({
      title: muted ? '确认禁言' : '解除禁言',
      content: `${muted ? '禁言后' : '解除禁言后'} ${member.nickname} ${muted ? '无法发送消息' : '可以发送消息'}。`,
      onOk: async () => {
        try {
          if (muted) {
            await api.post(`/api/v1/rooms/${roomId}/members/${member.user_id}/mute`)
          } else {
            await api.post(`/api/v1/rooms/${roomId}/members/${member.user_id}/unmute`)
          }
          message.success(muted ? '已禁言' : '已解除禁言')
          window.location.reload()
        } catch (error: any) {
          message.error(error.response?.data?.error || '操作失败')
        }
      },
    })
  }

  const handleSetAdmin = (member: RoomMember, isAdmin: boolean) => {
    Modal.confirm({
      title: isAdmin ? '设置管理员' : '撤销管理员',
      content: `${isAdmin ? '设置' : '撤销'} ${member.nickname} ${isAdmin ? '为管理员' : '管理员权限'}。`,
      onOk: async () => {
        try {
          if (isAdmin) {
            await api.post(`/api/v1/rooms/${roomId}/members/${member.user_id}/admin`)
          } else {
            await api.delete(`/api/v1/rooms/${roomId}/members/${member.user_id}/admin`)
          }
          message.success('操作成功')
          window.location.reload()
        } catch (error: any) {
          message.error(error.response?.data?.error || '操作失败')
        }
      },
    })
  }

  const handleTransferOwner = (member: RoomMember) => {
    Modal.confirm({
      title: '确认转让房主',
      content: `确定要将房主转让给 ${member.nickname} 吗？转让后你将变为普通成员。`,
      onOk: async () => {
        try {
          await api.post(`/api/v1/rooms/${roomId}/members/${member.user_id}/transfer`)
          message.success('转让成功')
          navigate('/')
        } catch (error: any) {
          message.error(error.response?.data?.error || '操作失败')
        }
      },
    })
  }

  const getMenuItems = (member: RoomMember): MenuProps['items'] => {
    const items: MenuProps['items'] = []

    if (member.role === 'owner' || !isAdmin) return items

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

    if (isOwner && member.role !== 'owner') {
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
      items.push({
        key: 'transfer',
        label: '转让房主',
        danger: true,
        onClick: () => handleTransferOwner(member),
      })
      items.push({
        type: 'divider',
      })
    }

    items.push({
      key: 'kick',
      label: '踢出房间',
      danger: true,
      onClick: () => handleKick(member),
    })

    return items
  }

  return (
    <div style={{ maxHeight: 500, overflowY: 'auto' }}>
      {members.map(member => (
        <div key={member.user_id} className="member-item">
          <Avatar size="default" src={member.avatar} icon={<UserOutlined />} />
          <div className="member-info">
            <div className="member-name">
              <span>{member.nickname}</span>
              <span className={`role-badge role-${member.role}`}>
                {member.role === 'owner' ? '房主' : member.role === 'admin' ? '管理员' : '成员'}
              </span>
              {!member.is_audio_on && <AudioMutedOutlined style={{ color: '#f5222d', fontSize: 14 }} />}
            </div>
          </div>
          {isAdmin && member.role !== 'owner' && (
            <Dropdown menu={{ items: getMenuItems(member) }} placement="bottomRight">
              <MoreOutlined style={{ cursor: 'pointer' }} />
            </Dropdown>
          )}
        </div>
      ))}
    </div>
  )
}
