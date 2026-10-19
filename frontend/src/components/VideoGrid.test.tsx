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

  it('prefers a stream with a live video track over an audio-only stream for the same user', () => {
    // 同一用户的两路流：先到达的是纯音频流（无视频轨道），后到达的携带视频。
    // 回归：旧实现盲取 userStreams[0]，拿到纯音频流导致视频窗口黑屏。
    const audioOnlyStream = {
      getVideoTracks: () => [],
    } as unknown as MediaStream
    const videoStream = {
      getVideoTracks: () => [{ readyState: 'live' }],
    } as unknown as MediaStream

    const { container } = render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={null}
        remoteStreams={new Map([[2, [audioOnlyStream, videoStream]]])}
      />
    )

    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video!.srcObject).toBe(videoStream)
  })

  it('falls back to the unattributed pool only for members without an attributed stream', () => {
    // 用户 2（Bob）没有已归属流，应从未识别池中按序取带视频轨道的流
    const audioOnlyStream = {
      getVideoTracks: () => [],
    } as unknown as MediaStream
    const videoStream = {
      getVideoTracks: () => [{ readyState: 'live' }],
    } as unknown as MediaStream

    const { container } = render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={null}
        remoteStreams={new Map([[0, [audioOnlyStream, videoStream]]])}
      />
    )

    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video!.srcObject).toBe(videoStream)
  })
})
