import { useRef, useCallback } from 'react'
import { message } from 'antd'
import { SFUConnection, getMediaStream } from '@/utils/webrtc'
import type { MediaE2EEManager } from '@/utils/mediaE2EE'

interface UseSFUMediaOptions {
  onRemoteTrack: (userId: number, track: MediaStreamTrack, stream: MediaStream) => void
  mediaE2EEManagerRef: React.MutableRefObject<MediaE2EEManager | null>
  localStreamRef: React.MutableRefObject<MediaStream | null>
}

export function useSFUMedia({ onRemoteTrack, mediaE2EEManagerRef, localStreamRef }: UseSFUMediaOptions) {
  const sfuConnectionRef = useRef<SFUConnection | null>(null)

  const connect = useCallback(async (
    roomId: number,
    userId: number,
    sfuToken: string,
  ) => {
    try {
      const stream = await getMediaStream(false, true)
      localStreamRef.current = stream
      localStreamRef.current.getVideoTracks().forEach(track => { track.enabled = false })
      localStreamRef.current.getAudioTracks().forEach(track => { track.enabled = true })

      const sfuConnection = new SFUConnection(
        roomId,
        userId,
        sfuToken,
        (userId: number, track: MediaStreamTrack, stream: MediaStream) => {
          onRemoteTrack(userId, track, stream)
        },
        () => {
          message.success('媒体连接已建立')
        },
        () => {
          console.log('[SFU] Connection disconnected')
        },
        (error: string) => {
          console.error('[SFU] Error:', error)
          message.error(error)
        },
        mediaE2EEManagerRef.current
      )

      sfuConnectionRef.current = sfuConnection
      await sfuConnection.connect(stream)
    } catch (error) {
      console.error('[SFU] Failed to get media stream:', error)
      message.error('获取摄像头/麦克风权限失败')
    }
  }, [onRemoteTrack, mediaE2EEManagerRef, localStreamRef])

  const disconnect = useCallback(() => {
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.disconnect()
      sfuConnectionRef.current = null
    }
  }, [])

  const setVideoSubscriptions = useCallback((userIds: number[]) => {
    sfuConnectionRef.current?.setVideoSubscriptions(userIds)
  }, [])

  return {
    sfuConnectionRef,
    connect,
    disconnect,
    setVideoSubscriptions,
  }
}
