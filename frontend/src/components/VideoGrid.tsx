import { useRef, useEffect } from 'react'
import { RoomMember } from '@/types'

interface VideoGridProps {
  members: RoomMember[]
  currentUserId: number
  localStream: MediaStream | null
  remoteStreams: Map<number, MediaStream[]>
}

export default function VideoGrid({
  members,
  currentUserId,
  localStream,
  remoteStreams,
}: VideoGridProps) {
  const localHasVideo = localStream?.getVideoTracks().some(t => t.enabled) || false
  const remoteVideoCount = members.filter(m => m.is_video_on && m.user_id !== currentUserId).length
  const videoCount = (localHasVideo ? 1 : 0) + remoteVideoCount

  let columns = 1
  if (videoCount >= 2 && videoCount < 4) columns = 2
  if (videoCount >= 4 && videoCount < 9) columns = 3
  if (videoCount >= 9) columns = 4

  const gridStyle = {
    gridTemplateColumns: `repeat(${columns}, 1fr)`,
  }

  const remoteMembers = members.filter(m => m.is_video_on && m.user_id !== currentUserId)

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
      {remoteMembers.map((member, index) => {
        // SFU 架构：所有远程流通过 SFU 转发，流统一存储在 key=0 中
        // 按成员索引分配流（当前 SFU 未实现 SSRC 与用户 ID 映射）
        const allRemoteStreams = remoteStreams.get(0) || []
        const userSpecificStreams = remoteStreams.get(member.user_id) || []
        const streams = userSpecificStreams.length > 0 ? userSpecificStreams : allRemoteStreams
        const stream = streams.length > index ? streams[index] : (streams[0] || null)
        return (
          <VideoCard
            key={member.user_id}
            stream={stream}
            nickname={member.nickname}
            isSelf={false}
            muted={member.is_muted}
          />
        )
      })}
    </div>
  )
}

interface VideoCardProps {
  stream: MediaStream | null
  nickname: string
  isSelf: boolean
  muted: boolean
}

function VideoCard({ stream, nickname, isSelf, muted }: VideoCardProps) {
  const videoRef = useRef<HTMLVideoElement>(null)

  useEffect(() => {
    if (videoRef.current && stream) {
      videoRef.current.srcObject = stream
    }
  }, [stream])

  return (
    <div className="video-container">
      {stream && (
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted={isSelf}
        />
      )}
      <div className="video-overlay">
        {nickname}
        {muted && <span> (已静音)</span>}
      </div>
    </div>
  )
}
