import { useRef, useEffect } from 'react'
import { RoomMember } from '@/types'

interface VideoGridProps {
  members: RoomMember[]
  currentUserId: number
  localStream: MediaStream | null
  remoteStreams: Map<number, MediaStream>
}

export default function VideoGrid({
  members,
  currentUserId,
  localStream,
  remoteStreams,
}: VideoGridProps) {
  const videoCount = members.filter(m => m.is_video_on).length + (localStream?.getVideoTracks().some(t => t.enabled) ? 1 : 0)

  let columns = 1
  if (videoCount >= 2 && videoCount < 4) columns = 2
  if (videoCount >= 4 && videoCount < 9) columns = 3
  if (videoCount >= 9) columns = 4

  const gridStyle = {
    gridTemplateColumns: `repeat(${columns}, 1fr)`,
  }

  const activeMembers = members.filter(m => m.is_video_on)

  return (
    <div className="video-grid" style={gridStyle}>
      {localStream && localStream.getVideoTracks().some(t => t.enabled) && (
        <VideoCard
          stream={localStream}
          nickname="你"
          isSelf
          muted={false}
        />
      )}
      {activeMembers.map(member => (
        <VideoCard
          key={member.user_id}
          stream={remoteStreams.get(member.user_id) || null}
          nickname={member.nickname}
          isSelf={member.user_id === currentUserId}
          muted={member.is_muted}
        />
      ))}
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
