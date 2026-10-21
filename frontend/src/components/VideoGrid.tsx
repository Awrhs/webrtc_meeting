import { useRef, useEffect, useState, memo, useMemo } from 'react'
import { RoomMember } from '@/types'

export interface RemoteStreamMeta {
  userId: number
  kind: string
  rid: string
}

interface VideoGridProps {
  members: RoomMember[]
  currentUserId: number
  localStream: MediaStream | null
  screenStream: MediaStream | null
  remoteStreams: Map<number, MediaStream[]>
  // 流元信息（streamId -> kind/rid）：rid 非空为摄像头（simulcast 层），空为屏幕共享/单层流
  remoteStreamMeta?: Map<string, RemoteStreamMeta>
}

function hasLiveVideo(stream: MediaStream): boolean {
  return stream.getVideoTracks().some(t => t.readyState === 'live')
}

export default memo(function VideoGrid({
  members,
  currentUserId,
  localStream,
  screenStream,
  remoteStreams,
  remoteStreamMeta,
}: VideoGridProps) {
  const localHasVideo = localStream?.getVideoTracks().some(t => t.enabled) || false

  // 按成员归类远端流：摄像头流与屏幕共享流分开
  const memberStreams = useMemo(() => {
    const cameras = new Map<number, MediaStream>()
    const screens = new Map<number, MediaStream>()
    for (const [userId, streams] of remoteStreams.entries()) {
      if (userId === 0) continue
      for (const stream of streams) {
        if (!hasLiveVideo(stream)) continue
        const meta = remoteStreamMeta?.get(stream.id)
        if (meta && meta.kind === 'video' && meta.rid === '') {
          // 无 rid 的视频流 = 屏幕共享（或非 simulcast 浏览器的摄像头）
          if (!screens.has(userId)) screens.set(userId, stream)
        } else {
          // 带 rid 的视频流 = 摄像头；无元信息时也按摄像头处理
          if (!cameras.has(userId)) cameras.set(userId, stream)
        }
      }
    }
    return { cameras, screens }
  }, [remoteStreams, remoteStreamMeta])

  const hasLocalScreenShare = screenStream !== null
  const firstRemoteScreen = useMemo(() => {
    for (const [userId, stream] of memberStreams.screens.entries()) {
      // 屏幕共享的成员即使没开摄像头也要展示其共享内容
      if (userId !== currentUserId) return stream
    }
    return null
  }, [memberStreams, currentUserId])

  // 大屏内容：优先远端共享（先到先得），其次本地共享
  const mainScreenStream = firstRemoteScreen ?? (hasLocalScreenShare ? screenStream : null)
  const hasScreenShareLayout = mainScreenStream !== null
  const mainScreenOwner = useMemo(() => {
    if (!hasScreenShareLayout) return ''
    if (mainScreenStream === screenStream && firstRemoteScreen === null) return '你'
    for (const [userId] of memberStreams.screens.entries()) {
      if (userId !== currentUserId) {
        const m = members.find(mm => mm.user_id === userId)
        return m?.nickname || `用户 ${userId}`
      }
    }
    return '远端成员'
  }, [hasScreenShareLayout, mainScreenStream, screenStream, firstRemoteScreen, memberStreams, members, currentUserId])

  const remoteMembersWithVideo = useMemo(
    () => members.filter(m => m.is_video_on && m.user_id !== currentUserId),
    [members, currentUserId]
  )

  if (hasScreenShareLayout) {
    return (
      <div className="video-grid-with-screen">
        <div className="screen-share-main">
          <VideoCard
            stream={mainScreenStream}
            nickname={mainScreenOwner === '你' ? '屏幕共享' : `${mainScreenOwner} 的屏幕共享`}
            isSelf={mainScreenStream === screenStream}
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

          {remoteMembersWithVideo.slice(0, 6).map((member) => {
            const stream = memberStreams.cameras.get(member.user_id)
            return (
              <VideoCard
                key={member.user_id}
                stream={stream ?? null}
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
        const stream = memberStreams.cameras.get(member.user_id)
        return (
          <VideoCard
            key={member.user_id}
            stream={stream ?? null}
            nickname={member.nickname || `用户 ${member.user_id}`}
            isSelf={false}
            muted={member.is_muted}
          />
        )
      })}
    </div>
  )
})

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
