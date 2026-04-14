export interface User {
  id: number
  username: string
  nickname: string
  email: string
  phone: string
  avatar: string
  status: string
}

export interface Room {
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

export interface RoomMember {
  id: number
  user_id: number
  nickname: string
  avatar: string
  role: 'owner' | 'admin' | 'member'
  is_muted: boolean
  is_video_on: boolean
  is_audio_on: boolean
}

export interface Message {
  id: number
  room_id: number
  sender_id: number
  sender_nickname: string
  sender_avatar: string
  receiver_id: number | null
  type: 'text' | 'image' | 'file' | 'system'
  content: string
  encrypted: boolean
  created_at: string
}

export interface SignalingMessage {
  type: string
  room_id: number
  user_id: number
  target_id: number
  data: any
}
