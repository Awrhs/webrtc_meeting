import { useRef, useCallback } from 'react'
import { message } from 'antd'
import { SFUConnection, getMediaStream } from '@/utils/webrtc'
import type { MediaE2EEManager } from '@/utils/mediaE2EE'

interface UseSFUMediaOptions {
  onRemoteTrack: (userId: number, track: MediaStreamTrack, stream: MediaStream) => void
  mediaE2EEManagerRef: React.MutableRefObject<MediaE2EEManager | null>
  localStreamRef: React.MutableRefObject<MediaStream | null>
  // 入会取得麦克风流后的回调（供 useLocalMedia 复用，避免重复申请麦克风）
  onInitialStream?: (stream: MediaStream) => void
  // 媒体连接建立/断开时的回调：页面据此显示持久的连接状态（瞬时的 toast 用户容易错过）
  onConnectionChange?: (connected: boolean) => void
}

export function useSFUMedia({ onRemoteTrack, mediaE2EEManagerRef, localStreamRef, onInitialStream, onConnectionChange }: UseSFUMediaOptions) {
  const sfuConnectionRef = useRef<SFUConnection | null>(null)
  // 期望的视频订阅集合：MeetingRoom 的订阅 effect 可能在 SFU 连接建立前就执行，
  // 先记录下来，连接创建后统一同步给 SFUConnection（joined 后重发）
  const desiredVideoSubscriptionsRef = useRef<number[]>([])

  const connect = useCallback(async (
    roomId: number,
    userId: number,
    sfuToken: string,
    sfuUrl?: string,
    iceServers?: RTCIceServer[],
  ) => {
    try {
      const stream = await getMediaStream(false, true)
      localStreamRef.current = stream
      localStreamRef.current.getVideoTracks().forEach(track => { track.enabled = false })
      localStreamRef.current.getAudioTracks().forEach(track => { track.enabled = true })
      onInitialStream?.(stream)

      const sfuConnection = new SFUConnection(
        roomId,
        userId,
        sfuToken,
        (userId: number, track: MediaStreamTrack, stream: MediaStream) => {
          onRemoteTrack(userId, track, stream)
        },
        () => {
          message.success('媒体连接已建立')
          onConnectionChange?.(true)
        },
        () => {
          console.log('[SFU] Connection disconnected')
          onConnectionChange?.(false)
        },
        (error: string) => {
          console.error('[SFU] Error:', error)
          message.error(error)
        },
        mediaE2EEManagerRef.current,
        sfuUrl || undefined,
        iceServers
      )

      sfuConnectionRef.current = sfuConnection
      // 把此前计算好的订阅意图同步给新连接（此时 WS 未开，发送会被丢弃，
      // 但 SFUConnection 会保留期望集合并在 joined 后统一重发）
      sfuConnection.setVideoSubscriptions(desiredVideoSubscriptionsRef.current)
      await sfuConnection.connect(stream)
    } catch (error) {
      console.error('[SFU] Failed to get media stream:', error)
      message.error('获取摄像头/麦克风权限失败')
    }
  }, [onRemoteTrack, mediaE2EEManagerRef, localStreamRef, onInitialStream, onConnectionChange])

  const disconnect = useCallback(() => {
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.disconnect()
      sfuConnectionRef.current = null
    }
  }, [])

  const setVideoSubscriptions = useCallback((userIds: number[]) => {
    desiredVideoSubscriptionsRef.current = userIds
    sfuConnectionRef.current?.setVideoSubscriptions(userIds)
  }, [])

  return {
    sfuConnectionRef,
    connect,
    disconnect,
    setVideoSubscriptions,
  }
}
