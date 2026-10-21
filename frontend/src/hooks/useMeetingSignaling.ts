import { useRef, useCallback } from 'react'
import { message } from 'antd'
import { SignalingClient } from '@/utils/signaling'
import { MediaE2EEManager, MediaE2EEState } from '@/utils/mediaE2EE'
import type { RoomMember } from '@/types'

interface UseMeetingSignalingCallbacks {
  dispatch: {
    setConnected: (connected: boolean) => any
    addMessage: (msg: any) => any
    updateMember: (member: RoomMember) => any
    removeMember: (userId: number) => any
  }
  onRemoveRemoteStreams: (userId: number) => void
  getActiveMemberIds: (excludeUserId?: number) => number[]
  currentUserId: number
  membersRef: React.MutableRefObject<RoomMember[]>
  leaveMeetingRef: React.MutableRefObject<(() => void) | null>
  navigateHomeRef: React.MutableRefObject<(() => void) | null>
  mediaE2EEManagerRef: React.MutableRefObject<MediaE2EEManager | null>
}

export function useMeetingSignaling(callbacks: UseMeetingSignalingCallbacks) {
  const signalingRef = useRef<SignalingClient | null>(null)

  const getMemberByUserId = useCallback((userId: number) => {
    return callbacks.membersRef.current.find(m => m.user_id === userId)
  }, [callbacks.membersRef])

  const connectAndJoin = useCallback(async (
    roomId: number,
    userId: number,
    members: RoomMember[],
    setMediaE2EEState: (state: MediaE2EEState) => void
  ) => {
    const signaling = new SignalingClient()
    const mediaE2EEManager = new MediaE2EEManager(true)
    callbacks.mediaE2EEManagerRef.current = mediaE2EEManager
    signaling.setMediaE2EEManager(mediaE2EEManager)
    signalingRef.current = signaling

    await signaling.connect()

    callbacks.dispatch.setConnected(true)
    mediaE2EEManager.init(
      (targetUserId, encryptedMediaKey) => {
        void signaling.sendMediaSenderKeyTo(targetUserId, encryptedMediaKey)
      },
      setMediaE2EEState
    )

    signaling.on('user_joined', (msg) => {
      message.info(`${msg.data?.user_name || '用户'}加入了房间`)
      if (msg.user_id !== callbacks.currentUserId) {
        void signaling.rotateForMembershipChange('join', msg.user_id, callbacks.getActiveMemberIds(msg.user_id))
      }
    })

    signaling.on('user_left', (msg) => {
      signaling.removeRoomMember(msg.user_id)
      callbacks.dispatch.removeMember(msg.user_id)
      callbacks.onRemoveRemoteStreams(msg.user_id)
      message.info(`${msg.data?.user_name || '用户'}离开了房间`)
      if (msg.user_id !== callbacks.currentUserId) {
        void signaling.rotateForMembershipChange(
          'leave',
          msg.user_id,
          callbacks.getActiveMemberIds().filter(uid => uid !== msg.user_id)
        )
      }
    })

    signaling.on('chat_message', (msg) => {
      callbacks.dispatch.addMessage(msg.data)
    })

    signaling.on('kicked', (msg) => {
      if (msg.target_id === callbacks.currentUserId) {
        message.error('你已被踢出房间')
        callbacks.leaveMeetingRef.current?.()
        callbacks.navigateHomeRef.current?.()
        return
      }
      signaling.removeRoomMember(msg.target_id)
      callbacks.dispatch.removeMember(msg.target_id)
      callbacks.onRemoveRemoteStreams(msg.target_id)
      void signaling.rotateForMembershipChange(
        'kick',
        msg.target_id,
        callbacks.getActiveMemberIds().filter(uid => uid !== msg.target_id)
      )
    })

    signaling.on('muted', (msg) => {
      const member = getMemberByUserId(msg.user_id)
      if (member) {
        callbacks.dispatch.updateMember({
          ...member,
          is_muted: msg.data.is_muted,
        })
      }
      if (msg.user_id === callbacks.currentUserId && msg.data.is_muted) {
        message.info('你已被禁言')
      }
    })

    signaling.on('role_changed', (msg) => {
      const member = getMemberByUserId(msg.user_id)
      if (member) {
        callbacks.dispatch.updateMember({
          ...member,
          role: msg.data.role,
        })
      }
    })

    signaling.on('media_state', (msg) => {
      if (msg.user_id !== callbacks.currentUserId) {
        const member = getMemberByUserId(msg.user_id)
        if (member) {
          callbacks.dispatch.updateMember({
            ...member,
            is_audio_on: msg.data.audio_enabled,
            is_video_on: msg.data.video_enabled,
          })
        }
      }
    })

    signaling.on('server_shutdown', () => {
      message.warning('服务器即将停机维护，连接将在稍后断开，请稍后重新进入会议', 8)
    })

    void signaling.joinRoom(roomId, userId, members)
  }, [callbacks, getMemberByUserId])

  const disconnect = useCallback(() => {
    if (signalingRef.current) {
      signalingRef.current.disconnect()
      signalingRef.current = null
    }
    if (callbacks.mediaE2EEManagerRef.current) {
      callbacks.mediaE2EEManagerRef.current.clear()
      callbacks.mediaE2EEManagerRef.current = null
    }
  }, [callbacks.mediaE2EEManagerRef])

  return {
    signalingRef,
    connectAndJoin,
    disconnect,
  }
}
