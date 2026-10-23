import { useState, useRef, useCallback } from 'react'
import { api } from '@/utils/api'
import { getMediaStream } from '@/utils/webrtc'
import type { SFUConnection } from '@/utils/webrtc'

interface UseLocalMediaOptions {
  sfuConnectionRef: React.MutableRefObject<SFUConnection | null>
  currentRoomId: number | undefined
}

export function useLocalMedia({ sfuConnectionRef, currentRoomId }: UseLocalMediaOptions) {
  const [videoOn, setVideoOn] = useState(false)
  const [audioOn, setAudioOn] = useState(true)
  const [screenShare, setScreenShare] = useState(false)
  const [localStream, setLocalStream] = useState<MediaStream | null>(null)
  const [screenStream, setScreenStream] = useState<MediaStream | null>(null)
  const [cameraDevices, setCameraDevices] = useState<MediaDeviceInfo[]>([])
  const [activeCameraId, setActiveCameraId] = useState<string>('')

  const localStreamRef = useRef<MediaStream | null>(null)
  const screenStreamRef = useRef<MediaStream | null>(null)

  // 由 useSFUMedia 在入会取得麦克风流后注入，供开启摄像头时合并轨道使用
  const setInitialStream = useCallback((stream: MediaStream) => {
    if (!localStreamRef.current) {
      localStreamRef.current = stream
      setLocalStream(stream)
    }
  }, [])

  const syncMediaState = useCallback((videoEnabled: boolean, audioEnabled: boolean) => {
    if (!currentRoomId) return
    api.put(`/api/v1/rooms/${currentRoomId}/media-state`, {
      video_enabled: videoEnabled,
      audio_enabled: audioEnabled,
    }).catch(() => {
      // sync failure is non-fatal
    })
  }, [currentRoomId])

  const toggleVideo = useCallback(async () => {
    const existing: MediaStream | null = localStreamRef.current
    if (!existing) {
      try {
        // 入会时的 stream 只含麦克风轨道。这里只申请摄像头视频（audio:false），
        // 再把已有麦克风轨道合入同一个 MediaStream，避免重复 getUserMedia 麦克风。
        const videoStream = await getMediaStream(true, false)
        const initialMic: MediaStream | null = (localStreamRef as React.MutableRefObject<MediaStream | null>).current
        const audioTracks: MediaStreamTrack[] = initialMic ? initialMic.getAudioTracks() : []
        const merged = new MediaStream([...audioTracks, ...videoStream.getVideoTracks()])
        localStreamRef.current = merged
        setLocalStream(merged)
        const videoTrack = videoStream.getVideoTracks()[0]
        if (videoTrack) {
          setActiveCameraId(videoTrack.getSettings().deviceId || '')
        }
        if (sfuConnectionRef.current && videoTrack) {
          // 传入 merged stream 以携带 msid：SFU 转发后订阅端靠 msid 把流归属到对应用户
          await sfuConnectionRef.current.replaceVideoTrack(videoTrack, merged)
        }
      } catch (error) {
        console.error('[useLocalMedia] Camera error:', error)
        return
      }
    } else {
      if (videoOn) {
        // 关闭摄像头：真正停止发布（不再发送黑帧浪费上行带宽）
        await sfuConnectionRef.current?.unpublishCamera()
      } else if (existing.getVideoTracks().length > 0) {
        // 重新开启：恢复到同一 sender（无需重新协商）
        const videoTrack = existing.getVideoTracks()[0]
        await sfuConnectionRef.current?.replaceVideoTrack(videoTrack, existing)
      }
    }
    const nextVideoOn = !videoOn
    setVideoOn(nextVideoOn)
    syncMediaState(nextVideoOn, audioOn)
  }, [videoOn, audioOn, sfuConnectionRef, syncMediaState])

  // 刷新可用摄像头列表（打开设备选择菜单时调用）
  const refreshCameraDevices = useCallback(async () => {
    try {
      const devices = await navigator.mediaDevices.enumerateDevices()
      setCameraDevices(devices.filter(d => d.kind === 'videoinput'))
    } catch (error) {
      console.error('[useLocalMedia] enumerateDevices failed:', error)
    }
  }, [])

  // 切换摄像头设备：replaceTrack 替换轨道（SSRC 不变，SFU 无需重新协商）
  const switchCamera = useCallback(async (deviceId: string) => {
    if (!localStreamRef.current) return
    try {
      const newStream = await navigator.mediaDevices.getUserMedia({
        video: { deviceId: { exact: deviceId } },
        audio: false,
      })
      const newTrack = newStream.getVideoTracks()[0]
      if (!newTrack) return
      newTrack.enabled = videoOn
      await sfuConnectionRef.current?.replaceVideoTrack(newTrack, localStreamRef.current)

      // 更新本地预览流：移除旧视频轨，加入新轨
      const oldTracks = localStreamRef.current.getVideoTracks()
      oldTracks.forEach(t => {
        localStreamRef.current!.removeTrack(t)
        t.stop()
      })
      localStreamRef.current.addTrack(newTrack)
      setLocalStream(new MediaStream(localStreamRef.current.getTracks()))
      setActiveCameraId(deviceId)
    } catch (error) {
      console.error('[useLocalMedia] switchCamera failed:', error)
    }
  }, [sfuConnectionRef, videoOn])

  const toggleAudio = useCallback(() => {
    if (!localStreamRef.current) return
    if (sfuConnectionRef.current) {
      sfuConnectionRef.current.muteAudio(audioOn)
    }
    const nextAudioOn = !audioOn
    setAudioOn(nextAudioOn)
    syncMediaState(videoOn, nextAudioOn)
  }, [audioOn, videoOn, sfuConnectionRef, syncMediaState])

  const toggleScreenShare = useCallback(async () => {
    if (!screenShare) {
      try {
        const stream = await navigator.mediaDevices.getDisplayMedia({
          video: true,
          audio: false,
        })
        screenStreamRef.current = stream
        setScreenStream(stream)
        const videoTrack = stream.getVideoTracks()[0]
        videoTrack.onended = () => {
          void stopScreenShare()
        }
        if (sfuConnectionRef.current) {
          await sfuConnectionRef.current.addVideoTrack(videoTrack, stream)
        }
        setScreenShare(true)
      } catch (error) {
        console.error('[useLocalMedia] Screen share error:', error)
      }
    } else {
      await stopScreenShare()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [screenShare, sfuConnectionRef])

  const stopScreenShare = useCallback(async () => {
    if (screenStreamRef.current) {
      screenStreamRef.current.getTracks().forEach(track => track.stop())
      screenStreamRef.current = null
    }
    if (sfuConnectionRef.current) {
      await sfuConnectionRef.current.removeScreenTrack()
    }
    setScreenStream(null)
    setScreenShare(false)
  }, [sfuConnectionRef])

  const cleanupLocalMedia = useCallback(() => {
    if (screenStreamRef.current) {
      screenStreamRef.current.getTracks().forEach(track => track.stop())
      screenStreamRef.current = null
    }
    if (localStreamRef.current) {
      localStreamRef.current.getTracks().forEach(track => track.stop())
      localStreamRef.current = null
    }
    setLocalStream(null)
    setVideoOn(false)
    setAudioOn(false)
    setScreenShare(false)
    setScreenStream(null)
  }, [])

  return {
    videoOn,
    audioOn,
    screenShare,
    localStream,
    screenStream,
    localStreamRef,
    screenStreamRef,
    cameraDevices,
    activeCameraId,
    toggleVideo,
    toggleAudio,
    toggleScreenShare,
    stopScreenShare,
    cleanupLocalMedia,
    refreshCameraDevices,
    switchCamera,
    setInitialStream,
  }
}
