import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import VideoGrid from './VideoGrid'
import { RoomMember } from '@/types'

const members: RoomMember[] = [
  {
    id: 1,
    user_id: 1,
    nickname: 'Alice',
    avatar: '',
    role: 'owner',
    is_muted: false,
    is_video_on: false,
    is_audio_on: true,
  },
  {
    id: 2,
    user_id: 2,
    nickname: 'Bob',
    avatar: '',
    role: 'member',
    is_muted: true,
    is_video_on: true,
    is_audio_on: true,
  },
]

describe('VideoGrid', () => {
  it('renders placeholder for remote member with video enabled but no stream yet', () => {
    render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={null}
        remoteStreams={new Map()}
      />
    )

    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('等待视频流...')).toBeInTheDocument()
    expect(screen.getByText('Bob (已静音)')).toBeInTheDocument()
  })

  it('renders screen share mode when screen stream exists', () => {
    const screenStream = {} as MediaStream

    render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={screenStream}
        remoteStreams={new Map()}
      />
    )

    expect(screen.getAllByText(/屏幕共享/).length).toBeGreaterThan(0)
  })
})
