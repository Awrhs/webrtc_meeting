import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit'
import { api } from '@/utils/api'
import { Message, Permission, JoinRoomResponse, RoomMember, Room } from '@/types'

interface RoomState {
  rooms: Room[]
  currentRoom: Room | null
  members: RoomMember[]
  messages: Message[]
  myPermissions: Permission[]
  loading: boolean
  connected: boolean
}

const initialState: RoomState = {
  rooms: [],
  currentRoom: null,
  members: [],
  messages: [],
  myPermissions: [],
  loading: false,
  connected: false,
}

export const getMyRooms = createAsyncThunk(
  'room/getMyRooms',
  async (_, { rejectWithValue }) => {
    try {
      const response = await api.get('/api/v1/rooms')
      return response.data.rooms
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const createRoom = createAsyncThunk(
  'room/createRoom',
  async (data: any, { rejectWithValue }) => {
    try {
      const response = await api.post('/api/v1/rooms', data)
      return response.data.room
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const getRoomByNumber = createAsyncThunk(
  'room/getRoomByNumber',
  async (roomNumber: string, { rejectWithValue }) => {
    try {
      const response = await api.get(`/api/v1/rooms/number/${roomNumber}`)
      return response.data.room
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const joinRoom = createAsyncThunk(
  'room/joinRoom',
  async ({ id, data }: { id: string; data: any }, { rejectWithValue }) => {
    try {
      const response = await api.post(`/api/v1/rooms/${id}/join`, data)
      return response.data
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const getRoomMessages = createAsyncThunk(
  'room/getMessages',
  async (roomId: number, { rejectWithValue }) => {
    try {
      const response = await api.get(`/api/v1/rooms/${roomId}/messages`)
      return response.data.messages
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 查询审计日志（owner/admin）
export const getAuditLogs = createAsyncThunk(
  'room/getAuditLogs',
  async ({ roomId, limit }: { roomId: number; limit?: number }, { rejectWithValue }) => {
    try {
      const response = await api.get(`/api/v1/rooms/${roomId}/audit-logs`, {
        params: { limit: limit || 50 },
      })
      return response.data.audit_logs
    } catch (error: any) {
      return rejectWithValue(error.response?.data?.error || '获取审计日志失败')
    }
  }
)

// 踢出成员
export const kickMember = createAsyncThunk(
  'room/kickMember',
  async ({ roomId, userId }: { roomId: number; userId: number }, { rejectWithValue }) => {
    try {
      await api.post(`/api/v1/rooms/${roomId}/members/${userId}/kick`)
      return userId
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 禁言/取消禁言成员
export const muteMember = createAsyncThunk(
  'room/muteMember',
  async ({ roomId, userId, muted }: { roomId: number; userId: number; muted: boolean }, { rejectWithValue }) => {
    try {
      const endpoint = muted ? 'mute' : 'unmute'
      await api.post(`/api/v1/rooms/${roomId}/members/${userId}/${endpoint}`)
      return { userId, muted }
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 设置/取消管理员
export const setAdmin = createAsyncThunk(
  'room/setAdmin',
  async ({ roomId, userId, isAdmin }: { roomId: number; userId: number; isAdmin: boolean }, { rejectWithValue }) => {
    try {
      if (isAdmin) {
        await api.post(`/api/v1/rooms/${roomId}/members/${userId}/admin`)
      } else {
        await api.delete(`/api/v1/rooms/${roomId}/members/${userId}/admin`)
      }
      return { userId, role: isAdmin ? 'admin' : 'member' }
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 转让房主
export const transferOwner = createAsyncThunk(
  'room/transferOwner',
  async ({ roomId, userId }: { roomId: number; userId: number }, { rejectWithValue }) => {
    try {
      await api.post(`/api/v1/rooms/${roomId}/members/${userId}/transfer`)
      return userId
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 锁定/解锁房间
export const lockRoom = createAsyncThunk(
  'room/lockRoom',
  async ({ roomId, locked }: { roomId: number; locked: boolean }, { rejectWithValue }) => {
    try {
      const endpoint = locked ? 'lock' : 'unlock'
      await api.post(`/api/v1/rooms/${roomId}/${endpoint}`)
      return locked
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 删除房间
export const deleteRoom = createAsyncThunk(
  'room/deleteRoom',
  async (roomId: number, { rejectWithValue }) => {
    try {
      await api.delete(`/api/v1/rooms/${roomId}`)
      return roomId
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

const roomSlice = createSlice({
  name: 'room',
  initialState,
  reducers: {
    setCurrentRoom: (state, action: PayloadAction<Room>) => {
      state.currentRoom = action.payload
    },
    setMembers: (state, action: PayloadAction<RoomMember[]>) => {
      state.members = action.payload
    },
    updateMember: (state, action: PayloadAction<RoomMember>) => {
      const index = state.members.findIndex(m => m.user_id === action.payload.user_id)
      if (index !== -1) {
        state.members[index] = action.payload
      } else {
        state.members.push(action.payload)
      }
    },
    removeMember: (state, action: PayloadAction<number>) => {
      state.members = state.members.filter(m => m.user_id !== action.payload)
    },
    addMessage: (state, action: PayloadAction<Message>) => {
      state.messages.push(action.payload)
    },
    setConnected: (state, action: PayloadAction<boolean>) => {
      state.connected = action.payload
    },
    clearCurrentRoom: (state) => {
      state.currentRoom = null
      state.members = []
      state.messages = []
      state.connected = false
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(getMyRooms.fulfilled, (state, action: PayloadAction<Room[]>) => {
        state.rooms = action.payload
      })
      .addCase(createRoom.fulfilled, (state, action: PayloadAction<Room>) => {
        state.rooms.unshift(action.payload)
      })
      .addCase(joinRoom.fulfilled, (state, action: PayloadAction<JoinRoomResponse>) => {
        state.currentRoom = action.payload.room
        state.members = action.payload.members
        state.myPermissions = action.payload.my_permissions
      })
      .addCase(getRoomMessages.fulfilled, (state, action: PayloadAction<Message[]>) => {
        state.messages = action.payload
      })
      .addCase(kickMember.fulfilled, (state, action: PayloadAction<number>) => {
        state.members = state.members.filter(m => m.user_id !== action.payload)
      })
      .addCase(muteMember.fulfilled, (state, action: PayloadAction<{ userId: number; muted: boolean }>) => {
        const member = state.members.find(m => m.user_id === action.payload.userId)
        if (member) {
          member.is_muted = action.payload.muted
        }
      })
      .addCase(setAdmin.fulfilled, (state, action: PayloadAction<{ userId: number; role: string }>) => {
        const member = state.members.find(m => m.user_id === action.payload.userId)
        if (member) {
          member.role = action.payload.role as any
        }
      })
      .addCase(lockRoom.fulfilled, (state, action: PayloadAction<boolean>) => {
        if (state.currentRoom) {
          state.currentRoom.is_locked = action.payload
        }
      })
      .addCase(deleteRoom.fulfilled, (state, action: PayloadAction<number>) => {
        state.rooms = state.rooms.filter(r => r.id !== action.payload)
        state.currentRoom = null
        state.members = []
        state.messages = []
        state.myPermissions = []
      })
  },
})

export const {
  setCurrentRoom,
  setMembers,
  updateMember,
  removeMember,
  addMessage,
  setConnected,
  clearCurrentRoom,
} = roomSlice.actions

export default roomSlice.reducer
