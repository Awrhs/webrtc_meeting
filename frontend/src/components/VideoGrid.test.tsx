import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import VideoGrid, { RemoteStreamMeta } from './VideoGrid'
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

const cameraMeta = new Map<string, RemoteStreamMeta>([
  ['cam-stream', { userId: 2, kind: 'video', rid: 'high' }],
])
const screenMeta = new Map<string, RemoteStreamMeta>([
  ['screen-stream', { userId: 2, kind: 'video', rid: '' }],
])

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

  it('renders screen share mode when local screen stream exists', () => {
    const screenStream = { id: 'local-screen' } as MediaStream

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
    const audioOnlyStream = { id: 'audio-stream', getVideoTracks: () => [] } as unknown as MediaStream
    const videoStream = {
      id: 'cam-stream',
      getVideoTracks: () => [{ readyState: 'live' }],
    } as unknown as MediaStream

    const { container } = render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={null}
        remoteStreams={new Map([[2, [audioOnlyStream, videoStream]]])}
        remoteStreamMeta={cameraMeta}
      />
    )

    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video!.srcObject).toBe(videoStream)
  })

  it('renders remote screen share in the main area (large layout)', () => {
    // Bob 既开了摄像头又在共享屏幕：大屏应显示其屏幕流，侧边栏显示摄像头流
    const cameraStream = {
      id: 'cam-stream',
      getVideoTracks: () => [{ readyState: 'live' }],
    } as unknown as MediaStream
    const screenStream = {
      id: 'screen-stream',
      getVideoTracks: () => [{ readyState: 'live' }],
    } as unknown as MediaStream

    const meta = new Map(cameraMeta)
    meta.set('screen-stream', screenMeta.get('screen-stream')!)

    render(
      <VideoGrid
        members={members}
        currentUserId={1}
        localStream={null}
        screenStream={null}
        remoteStreams={new Map([[2, [cameraStream, screenStream]]])}
        remoteStreamMeta={meta}
      />
    )

    // 远端共享应出现在大屏（带“的屏幕共享”标签）
    expect(screen.getByText(/Bob 的屏幕共享/)).toBeInTheDocument()

    // 该成员的摄像头仍在侧边栏
    const videos = document.querySelectorAll('.screen-share-sidebar video')
    expect(videos.length).toBe(1)
  })
})
