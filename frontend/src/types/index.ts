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
  invite_code?: string
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
  target_id?: number
  data: any
}

// 权限枚举
export type Permission =
  | 'kick_member'
  | 'mute_member'
  | 'set_admin'
  | 'lock_room'
  | 'delete_room'
  | 'transfer_owner'
  | 'send_message'
  | 'update_media_state'
  | 'view_room_info'
  | 'view_member_list'

// E2EE 端到端加密相关类型
export interface E2EEPublicKey {
  user_id: number
  public_key: string
  key_type: string
  exists: boolean
}

// 加入房间响应类型
export interface JoinRoomResponse {
  room: Room
  members: RoomMember[]
  my_permissions: Permission[]
  sfu_token: string
  sfu_url?: string
  // 服务端下发的 ICE 配置：STUN + 时限 TURN 凭据（coturn --use-auth-secret），固定账号不再打进前端
  ice_servers?: RTCIceServer[]
  ice_ttl_seconds?: number
}

// 审计日志
export interface AuditLog {
  id: number
  room_id: number
  user_id: number
  target_id: number | null
  action: string
  details: string
  ip_address: string
  user_agent: string
  created_at: string
}

// 登录设备记录
export interface Device {
  id: number
  user_id: number
  device_id: string
  device_name: string
  device_type: string
  user_agent: string
  ip: string
  last_active: string
  created_at: string
}

// 文件上传响应
export interface UploadResult {
  url: string
  name: string
  size: number
  type: 'image' | 'file' | 'text' | 'system'
}
