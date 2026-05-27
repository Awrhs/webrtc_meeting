import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useRemoteStreams } from '@/hooks/useRemoteStreams'

function makeMockStream(id: string): MediaStream {
  return { id, active: true } as unknown as MediaStream
}

describe('useRemoteStreams', () => {
  it('adds remote streams for a user', () => {
    const { result } = renderHook(() => useRemoteStreams())
    const stream = makeMockStream('s1')

    act(() => {
      result.current.addRemoteStream(1, stream)
    })

    expect(result.current.remoteStreams.get(1)).toContain(stream)
  })

  it('does not add duplicate streams', () => {
    const { result } = renderHook(() => useRemoteStreams())
    const stream = makeMockStream('s1')

    act(() => {
      result.current.addRemoteStream(1, stream)
      result.current.addRemoteStream(1, stream)
    })

    expect(result.current.remoteStreams.get(1)?.length).toBe(1)
  })

  it('removes all streams for a user', () => {
    const { result } = renderHook(() => useRemoteStreams())
    const s1 = makeMockStream('s1')
    const s2 = makeMockStream('s2')

    act(() => {
      result.current.addRemoteStream(1, s1)
      result.current.addRemoteStream(1, s2)
      result.current.removeRemoteStreamsByUser(1)
    })

    expect(result.current.remoteStreams.has(1)).toBe(false)
  })

  it('clears all remote streams', () => {
    const { result } = renderHook(() => useRemoteStreams())

    act(() => {
      result.current.addRemoteStream(1, makeMockStream('s1'))
      result.current.addRemoteStream(2, makeMockStream('s2'))
      result.current.clearRemoteStreams()
    })

    expect(result.current.remoteStreams.size).toBe(0)
  })

  it('removes a specific stream by userId and stream', () => {
    const { result } = renderHook(() => useRemoteStreams())
    const s1 = makeMockStream('s1')
    const s2 = makeMockStream('s2')

    act(() => {
      result.current.addRemoteStream(1, s1)
      result.current.addRemoteStream(1, s2)
      result.current.removeRemoteStream(1, s1)
    })

    expect(result.current.remoteStreams.get(1)).toEqual([s2])
  })

  it('removes userId entry when last stream is removed', () => {
    const { result } = renderHook(() => useRemoteStreams())
    const s1 = makeMockStream('s1')

    act(() => {
      result.current.addRemoteStream(1, s1)
      result.current.removeRemoteStream(1, s1)
    })

    expect(result.current.remoteStreams.has(1)).toBe(false)
  })
})
