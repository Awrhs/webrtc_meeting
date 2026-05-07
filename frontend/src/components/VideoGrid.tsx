import { useRef, useEffect, useState } from 'react'
import { RoomMember } from '@/types'

interface VideoGridProps {
  members: RoomMember[]
  currentUserId: number
  localStream: MediaStream | null
  screenStream: MediaStream | null
  remoteStreams: Map<number, MediaStream[]>
}

/**
 * 视频网格组件
 *
 * 架构说明：
 * - remoteStreams 按 userId 作为 key，每个 userId 对应一个流数组
 * - 如果 userId = 0，表示这些流还没有被后端映射（向后兼容）
 * - 支持屏幕共享（大屏模式） + 侧边摄像头小窗
 */
export default function VideoGrid({
  members,
  currentUserId,
  localStream,
  screenStream,
  remoteStreams,
}: VideoGridProps) {
  const localHasVideo = localStream?.getVideoTracks().some(t => t.enabled) || false
  const hasScreenShare = screenStream !== null

  // 获取所有开启视频的远程用户（排除自己）
  const remoteMembersWithVideo = members.filter(
    m => m.is_video_on && m.user_id !== currentUserId
  )

  // 如果有屏幕共享，使用大屏模式
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

  // 普通网格布局
  const totalVideoCount = (localHasVideo ? 1 : 0) + remoteMembersWithVideo.length

  let columns = 1
  if (totalVideoCount >= 2 && totalVideoCount < 4) columns = 2
  if (totalVideoCount >= 4 && totalVideoCount < 9) columns = 3
  if (totalVideoCount >= 9) columns = 4

  const gridStyle = {
    gridTemplateColumns: `repeat(${columns}, 1fr)`,
  }

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
}

/**
 * 获取指定用户的远程流
 *
 * 优先级：
 * 1. 优先使用精确匹配 userId 的流（后端已实现 SSRC 映射时）
 * 2. 否则从 userId=0 的公共池中按索引分配（向后兼容）
 */
function getRemoteStreamForUser(
  userId: number,
  remoteMembersWithVideo: RoomMember[],
  remoteStreams: Map<number, MediaStream[]>
): MediaStream | null {
  // 1. 优先查找精确匹配
  const userStreams = remoteStreams.get(userId)
  if (userStreams && userStreams.length > 0) {
    return userStreams[0]
  }

  // 2. 向后兼容：从公共池按索引分配
  const poolStreams = remoteStreams.get(0) || []
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

function VideoCard({ stream, nickname, isSelf, muted, isScreenShare }: VideoCardProps) {
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
}
