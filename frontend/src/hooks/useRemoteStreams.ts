import { useReducer, useCallback } from 'react'

interface RemoteStreamsState {
  streams: Map<number, MediaStream[]>
  version: number
}

type RemoteStreamsAction =
  | { type: 'ADD_STREAM'; userId: number; stream: MediaStream }
  | { type: 'REMOVE_USER'; userId: number }
  | { type: 'REMOVE_STREAM'; userId: number; stream: MediaStream }
  | { type: 'CLEAR' }

function remoteStreamsReducer(state: RemoteStreamsState, action: RemoteStreamsAction): RemoteStreamsState {
  switch (action.type) {
    case 'ADD_STREAM': {
      const existing = state.streams.get(action.userId) || []
      if (existing.includes(action.stream)) return state
      const next = new Map(state.streams)
      next.set(action.userId, [...existing, action.stream])
      return { streams: next, version: state.version + 1 }
    }
    case 'REMOVE_USER': {
      if (!state.streams.has(action.userId)) return state
      const next = new Map(state.streams)
      next.delete(action.userId)
      return { streams: next, version: state.version + 1 }
    }
    case 'REMOVE_STREAM': {
      const existing = state.streams.get(action.userId)
      if (!existing) return state
      const filtered = existing.filter(s => s.id !== action.stream.id)
      if (filtered.length === existing.length) return state
      const next = new Map(state.streams)
      if (filtered.length === 0) {
        next.delete(action.userId)
      } else {
        next.set(action.userId, filtered)
      }
      return { streams: next, version: state.version + 1 }
    }
    case 'CLEAR':
      return { streams: new Map(), version: 0 }
    default:
      return state
  }
}

export function useRemoteStreams() {
  const [state, dispatch] = useReducer(remoteStreamsReducer, { streams: new Map(), version: 0 })

  const addRemoteStream = useCallback((userId: number, stream: MediaStream) => {
    dispatch({ type: 'ADD_STREAM', userId, stream })
  }, [])

  const removeRemoteStreamsByUser = useCallback((userId: number) => {
    dispatch({ type: 'REMOVE_USER', userId })
  }, [])

  const removeRemoteStream = useCallback((userId: number, stream: MediaStream) => {
    dispatch({ type: 'REMOVE_STREAM', userId, stream })
  }, [])

  const clearRemoteStreams = useCallback(() => {
    dispatch({ type: 'CLEAR' })
  }, [])

  return {
    remoteStreams: state.streams,
    addRemoteStream,
    removeRemoteStreamsByUser,
    removeRemoteStream,
    clearRemoteStreams,
  }
}
