import { createSlice, createAsyncThunk, PayloadAction } from '@reduxjs/toolkit'
import { api } from '@/utils/api'
import { clearLegacyKeyStore } from '@/utils/encryption'

interface User {
  id: number
  username: string
  nickname: string
  email: string
  phone: string
  avatar: string
  status: string
}

interface AuthState {
  user: User | null
  token: string | null
  isAuthenticated: boolean
  loading: boolean
}

const initialState: AuthState = {
  user: null,
  token: localStorage.getItem('token'),
  isAuthenticated: !!localStorage.getItem('token'),
  loading: false,
}

export const login = createAsyncThunk(
  'auth/login',
  async (credentials: { account: string; password: string }, { rejectWithValue }) => {
    try {
      const response = await api.post('/api/v1/auth/login', credentials)
      return response.data
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const register = createAsyncThunk(
  'auth/register',
  async (data: { username: string; email: string; phone: string; password: string }, { rejectWithValue }) => {
    try {
      const response = await api.post('/api/v1/auth/register', data)
      return response.data
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const getMe = createAsyncThunk(
  'auth/getMe',
  async (_, { rejectWithValue }) => {
    try {
      const response = await api.get('/api/v1/user/me')
      return response.data
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

export const updateProfile = createAsyncThunk(
  'auth/updateProfile',
  async (data: { nickname: string; avatar: string }, { rejectWithValue }) => {
    try {
      const response = await api.put('/api/v1/user/profile', data)
      return response.data
    } catch (error: any) {
      return rejectWithValue(error.response.data.error)
    }
  }
)

// 登出：先通知服务端吊销当前会话（token 立即失效，其它设备不受影响），再清理本地状态。
// 服务端不可达时也要能登出，所以吊销失败只记录不阻断。
export const logoutAndRevoke = createAsyncThunk('auth/logoutAndRevoke', async (_, { dispatch }) => {
  try {
    await api.post('/api/v1/auth/logout')
  } catch (error) {
    console.warn('[auth] server-side logout failed, clearing local session anyway', error)
  }
  dispatch(logout())
})

const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    logout: (state) => {
      // 显式登出：销毁本设备的 E2EE 密钥（历史加密消息将不可再解密）
      clearLegacyKeyStore()
      localStorage.removeItem('token')
      state.user = null
      state.token = null
      state.isAuthenticated = false
    },
    // 改密码后服务端会返回为当前设备签发的新 token：替换本地凭据，不必重新登录
    setToken: (state, action: PayloadAction<string>) => {
      state.token = action.payload
      state.isAuthenticated = true
      localStorage.setItem('token', action.payload)
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(login.pending, (state) => {
        state.loading = true
      })
      .addCase(login.fulfilled, (state, action: PayloadAction<any>) => {
        state.loading = false
        state.token = action.payload.token
        state.user = action.payload.user
        state.isAuthenticated = true
        localStorage.setItem('token', action.payload.token)
      })
      .addCase(login.rejected, (state) => {
        state.loading = false
      })
      .addCase(getMe.fulfilled, (state, action: PayloadAction<any>) => {
        state.user = action.payload.user
      })
      .addCase(updateProfile.fulfilled, (state, action) => {
        if (state.user) {
          state.user.nickname = action.meta.arg.nickname
          if (action.meta.arg.avatar) {
            state.user.avatar = action.meta.arg.avatar
          }
        }
      })
  },
})

export const { logout, setToken } = authSlice.actions
export default authSlice.reducer
