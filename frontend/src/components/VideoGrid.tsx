import { useRef, useEffect, useState, memo, useMemo } from 'react'
import { RoomMember } from '@/types'

interface VideoGridProps {
  members: RoomMember[]
  currentUserId: number
  localStream: MediaStream | null
  screenStream: MediaStream | null
  remoteStreams: Map<number, MediaStream[]>
}

export default memo(function VideoGrid({
  members,
  currentUserId,
  localStream,
  screenStream,
  remoteStreams,
}: VideoGridProps) {
  const localHasVideo = localStream?.getVideoTracks().some(t => t.enabled) || false
  const hasScreenShare = screenStream !== null

  const remoteMembersWithVideo = useMemo(
    () => members.filter(m => m.is_video_on && m.user_id !== currentUserId),
    [members, currentUserId]
  )

  if (hasScreenShare) {
    return (
      <div className="video-grid-with-screen">
        <div className="screen-share-main">
          <VideoCard
            stream={screenStream}
            nickname="屏幕共享"
            isSelf
            muted={false}
            isScreenShare
          />
        </div>

        <div className="screen-share-sidebar">
          {localHasVideo && (
            <VideoCard
              stream={localStream}
              nickname="你"
              isSelf
              muted={false}
            />
          )}

          {remoteMembersWithVideo.slice(0, 3).map((member) => {
            const stream = getRemoteStreamForUser(
              member.user_id,
              remoteMembersWithVideo,
              remoteStreams
            )
            return (
              <VideoCard
                key={member.user_id}
                stream={stream}
                nickname={member.nickname || `用户 ${member.user_id}`}
                isSelf={false}
                muted={member.is_muted}
              />
            )
          })}
        </div>
      </div>
    )
  }

  const totalVideoCount = (localHasVideo ? 1 : 0) + remoteMembersWithVideo.length

  const columns = useMemo(() => {
    if (totalVideoCount >= 9) return 4
    if (totalVideoCount >= 4) return 3
    if (totalVideoCount >= 2) return 2
    return 1
  }, [totalVideoCount])

  const gridStyle = useMemo(() => ({
    gridTemplateColumns: `repeat(${columns}, 1fr)`,
  }), [columns])

  return (
    <div className="video-grid" style={gridStyle}>
      {localHasVideo && (
        <VideoCard
          stream={localStream}
          nickname="你"
          isSelf
          muted={false}
        />
      )}

      {remoteMembersWithVideo.map((member) => {
        const stream = getRemoteStreamForUser(
          member.user_id,
          remoteMembersWithVideo,
          remoteStreams
        )
        return (
          <VideoCard
            key={member.user_id}
            stream={stream}
            nickname={member.nickname || `用户 ${member.user_id}`}
            isSelf={false}
            muted={member.is_muted}
          />
        )
      })}
    </div>
  )
})

function getRemoteStreamForUser(
  userId: number,
  remoteMembersWithVideo: RoomMember[],
  remoteStreams: Map<number, MediaStream[]>
): MediaStream | null {
  const userStreams = remoteStreams.get(userId)
  if (userStreams && userStreams.length > 0) {
    // 同一用户可能有多路流（音频流 + 摄像头/屏幕流是不同的 msid），
    // 优先取带活跃视频轨道的流，避免拿到纯音频流导致黑屏。
    const withVideo = userStreams.find(
      s => s.getVideoTracks().some(t => t.readyState === 'live')
    )
    return withVideo ?? userStreams[0]
  }

  const poolStreams = (remoteStreams.get(0) || []).filter(
    s => s.getVideoTracks().some(t => t.readyState === 'live')
  )
  const memberIndex = remoteMembersWithVideo.findIndex(m => m.user_id === userId)
  if (memberIndex !== -1 && poolStreams.length > memberIndex) {
    return poolStreams[memberIndex]
  }

  return null
}

interface VideoCardProps {
  stream: MediaStream | null
  nickname: string
  isSelf: boolean
  muted: boolean
  isScreenShare?: boolean
}

const VideoCard = memo(function VideoCard({ stream, nickname, isSelf, muted, isScreenShare }: VideoCardProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [streamError, setStreamError] = useState(false)

  useEffect(() => {
    if (videoRef.current && stream) {
      videoRef.current.srcObject = stream
      setStreamError(false)
    }
  }, [stream])

  if (!stream || streamError) {
    return (
      <div className="video-container video-placeholder-container">
        <div className="video-placeholder">
          <div className="user-avatar">{nickname[0]}</div>
          <div className="user-name">{nickname}</div>
          {!stream && <div className="waiting-indicator">等待视频流...</div>}
          {streamError && <div className="error-indicator">视频加载失败</div>}
        </div>
        <div className="video-overlay">
          {isScreenShare && '📺 '}
          {nickname}
          {muted && ' (已静音)'}
        </div>
      </div>
    )
  }

  return (
    <div className={`video-container ${isScreenShare ? 'screen-share-container' : ''}`}>
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted={isSelf}
        onError={() => setStreamError(true)}
      />
      <div className="video-overlay">
        {isScreenShare && '📺 '}
        {nickname}
        {muted && ' (已静音)'}
      </div>
    </div>
  )
})
