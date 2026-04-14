import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit'
import { api } from '@/utils/api'
import { Message } from '@/types'

interface RoomMember {
  id: number
  user_id: number
  nickname: string
  avatar: string
  role: 'owner' | 'admin' | 'member'
  is_muted: boolean
  is_video_on: boolean
  is_audio_on: boolean
}

interface Room {
  id: number
  room_id: string
  name: string
  topic: string
  owner_id: number
  max_members: number
  is_permanent: boolean
  is_locked: boolean
  auth_type: 'none' | 'password' | 'invite_code'
  current_size: number
}

interface RoomState {
  rooms: Room[]
  currentRoom: Room | null
  members: RoomMember[]
  messages: Message[]
  loading: boolean
  connected: boolean
}

const initialState: RoomState = {
  rooms: [],
  currentRoom: null,
  members: [],
  messages: [],
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
      .addCase(joinRoom.fulfilled, (state, action: PayloadAction<any>) => {
        state.currentRoom = action.payload.room
        state.members = action.payload.members
      })
      .addCase(getRoomMessages.fulfilled, (state, action: PayloadAction<Message[]>) => {
        state.messages = action.payload
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
