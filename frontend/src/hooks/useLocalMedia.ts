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

  const localStreamRef = useRef<MediaStream | null>(null)
  const screenStreamRef = useRef<MediaStream | null>(null)

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
    if (!localStreamRef.current) {
      try {
        const stream = await getMediaStream(true, audioOn)
        localStreamRef.current = stream
        setLocalStream(stream)
        const videoTrack = stream.getVideoTracks()[0]
        if (sfuConnectionRef.current) {
          await sfuConnectionRef.current.replaceVideoTrack(videoTrack)
        }
      } catch (error) {
        console.error('[useLocalMedia] Camera error:', error)
        return
      }
    } else {
      if (sfuConnectionRef.current) {
        sfuConnectionRef.current.muteVideo(videoOn)
      }
    }
    const nextVideoOn = !videoOn
    setVideoOn(nextVideoOn)
    syncMediaState(nextVideoOn, audioOn)
  }, [videoOn, audioOn, sfuConnectionRef, syncMediaState])

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
    toggleVideo,
    toggleAudio,
    toggleScreenShare,
    stopScreenShare,
    cleanupLocalMedia,
  }
}
