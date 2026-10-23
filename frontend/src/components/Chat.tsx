import { useState, useEffect, useRef, useMemo } from 'react'
import { useSelector } from 'react-redux'
import { Button, Input, Space, message, Tag, Tooltip, Modal } from 'antd'
import {
  SendOutlined,
  LockOutlined,
  InfoCircleOutlined,
  SafetyCertificateOutlined,
  PaperClipOutlined,
} from '@ant-design/icons'
import { RootState } from '@/store'
import { Message, RoomMember, UploadResult } from '@/types'
import { groupE2EEManager, EncryptedMessage } from '@/utils/encryption'
import { api } from '@/utils/api'

interface ChatProps {
  roomId: number
  messages: Message[]
  signaling?: any
  privateTarget?: RoomMember | null
  onClosePrivate?: () => void
}

export default function Chat({ roomId, messages, signaling, privateTarget, onClosePrivate }: ChatProps) {
  const { user } = useSelector((state: RootState) => state.auth)
  const { members } = useSelector((state: RootState) => state.room)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [e2eeReady, setE2eeReady] = useState(false)
  const [myFingerprint, setMyFingerprint] = useState('')
  const [debugInfo, setDebugInfo] = useState('')
  const [decryptedMessages, setDecryptedMessages] = useState<Record<number, string>>({})
  const [fingerprintModalVisible, setFingerprintModalVisible] = useState(false)
  const [memberFingerprints, setMemberFingerprints] = useState<Array<{ userId: number; name: string; fingerprint: string }>>([])

  const messagesEndRef = useRef<HTMLDivElement>(null)
  // 自己发出的消息无法用自己的 Sender Key 解密（链已棘轮），发送时记住明文。
  // 密文（含随机 IV）全局唯一，作为 key 可以可靠地把回显消息映射回明文。
  const sentPlaintexts = useRef<Map<string, string>>(new Map())
  // 文件/图片上传状态
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  // 私聊历史已加载标记
  const privateHistoryLoaded = useRef<Set<number>>(new Set())
  // 私聊会话的追加消息（store 只保存群聊历史 + 实时消息）
  const [privateMessages, setPrivateMessages] = useState<Record<number, Message[]>>({})

  useEffect(() => {
    groupE2EEManager.setCallbacks(
      (targetUserId: number, encryptedChainKey: string) => {
        console.log(`[Chat] Distributing sender key to user ${targetUserId}`)
        if (signaling) {
          signaling.send({
            type: 'sender_key',
            room_id: roomId,
            user_id: user?.id,
            target_id: targetUserId,
            data: {
              from_user: user?.id,
              encrypted_chain_key: encryptedChainKey,
            },
          })
        }
      },
      (ready: boolean) => {
        setE2eeReady(ready)
        if (ready) {
          setDebugInfo('端到端加密已就绪')
          groupE2EEManager.saveToStorage(user?.id || 0)
        }
      },
      (fingerprint: string) => {
        setMyFingerprint(fingerprint)
      }
    )

    if (!signaling) {
      groupE2EEManager.init(user?.id).catch(console.error)
    }

    // 监听 Sender Key 分发消息
    if (signaling) {
      signaling.on('sender_key', async (msg: any) => {
        if (msg.data?.key_type === 'media') return
        if (msg.target_id && msg.target_id !== user?.id) return
        try {
          await groupE2EEManager.receiveSenderKey(
            msg.data.from_user,
            msg.data.encrypted_chain_key
          )
          console.log(`[Chat] Received sender key from user ${msg.data.from_user}`)
        } catch (err) {
          console.error('[Chat] Failed to receive sender key:', err)
        }
      })
    }

    return () => {}
  }, [roomId, user?.id, signaling])

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  useEffect(() => {
    messages.forEach(message => {
      if (!message.encrypted || decryptedMessages[message.id]) return

      // 自己发送的消息：直接取发送时记录的明文（无需也无法解密）
      if (message.sender_id === user?.id) {
        const plaintext = sentPlaintexts.current.get(message.content)
        if (plaintext !== undefined) {
          setDecryptedMessages(prev => ({ ...prev, [message.id]: plaintext }))
        }
        return
      }

      try {
        const encryptedData = JSON.parse(message.content) as EncryptedMessage
        groupE2EEManager.decryptMessage(encryptedData)
          .then(plaintext => {
            setDecryptedMessages(prev => ({ ...prev, [message.id]: plaintext }))
          })
          .catch(() => {})
      } catch {
        setDecryptedMessages(prev => ({ ...prev, [message.id]: '[消息格式错误]' }))
      }
    })
  }, [messages, decryptedMessages, user?.id])

  const showFingerprintModal = async () => {
    // 列出全部成员的指纹（自己的 + 已建立共享密钥的成员），支持线下比对防中间人。
    // 使用异步方法实时计算，避免同步缓存未刷新时列表为空。
    const fingerprints: Array<{ userId: number; name: string; fingerprint: string }> = [
      { userId: user?.id || 0, name: '你', fingerprint: myFingerprint },
    ]
    for (const member of members) {
      if (member.user_id === user?.id) continue
      const fingerprint = await groupE2EEManager.getMemberFingerprintAsync(member.user_id)
      if (fingerprint) {
        fingerprints.push({ userId: member.user_id, name: member.nickname || `用户 ${member.user_id}`, fingerprint })
      }
    }
    setMemberFingerprints(fingerprints)
    setFingerprintModalVisible(true)
  }

  // 打开私聊时加载历史（每个目标只拉一次）
  useEffect(() => {
    if (!privateTarget) return
    if (privateHistoryLoaded.current.has(privateTarget.user_id)) return
    privateHistoryLoaded.current.add(privateTarget.user_id)
    ;(async () => {
      try {
        const response = await api.get(`/api/v1/rooms/${roomId}/messages/private/${privateTarget.user_id}`)
        const list = (response.data?.messages || []) as Message[]
        setPrivateMessages(prev => ({ ...prev, [privateTarget.user_id]: list }))
      } catch {
        // 历史加载失败时仅显示实时消息
      }
    })()
  }, [privateTarget, roomId])

  const handleSend = async () => {
    if (!input.trim()) return
    if (!user) return

    setSending(true)
    try {
      let content = input.trim()
      let encrypted = false
      let encryptionData: EncryptedMessage | null = null

      // 群聊文本启用 E2EE；私聊/文件消息不加密（密钥链仅覆盖群聊成员间文本）
      if (e2eeReady && !privateTarget) {
        encryptionData = await groupE2EEManager.encryptMessage(content, user.id)
        content = JSON.stringify(encryptionData)
        encrypted = true
        setDebugInfo(`消息 #${encryptionData.messageNumber} 已加密`)
        // 记住明文：回显的加密消息无法用自己的链解密
        sentPlaintexts.current.set(content, input.trim())
        await groupE2EEManager.saveToStorage(user.id)
      }

      await api.post(`/api/v1/rooms/${roomId}/messages`, {
        type: 'text',
        content,
        encrypted,
        receiver_id: privateTarget?.user_id,
      })

      setInput('')
    } catch (error: any) {
      console.error('Failed to send message', error)
      message.error(error?.response?.data?.error || '消息发送失败')
    } finally {
      setSending(false)
    }
  }

  // 上传并发送图片/文件消息
  const handleFileSelected = async (file: File) => {
    if (!user) return
    setUploading(true)
    try {
      const formData = new FormData()
      formData.append('file', file)
      const response = await api.post(`/api/v1/rooms/${roomId}/upload`, formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })
      const upload = response.data as UploadResult
      await api.post(`/api/v1/rooms/${roomId}/messages`, {
        type: upload.type,
        content: JSON.stringify({ url: upload.url, name: upload.name, size: upload.size }),
        encrypted: false,
        receiver_id: privateTarget?.user_id,
      })
    } catch (error: any) {
      console.error('Failed to upload file', error)
      message.error(error?.response?.data?.error || '文件发送失败')
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const getMessageContent = (message: Message): string => {
    if (!message.encrypted) {
      return message.content
    }
    return decryptedMessages[message.id] || '[已加密消息 - 正在解密...]'
  }

  // 后端广播/历史接口都不回填 sender_nickname（模型未联表），
  // 前端从成员列表/当前用户解析发送者昵称
  const senderName = (message: Message): string => {
    if (message.sender_id === user?.id) {
      return user?.nickname || '你'
    }
    const member = members.find(m => m.user_id === message.sender_id)
    return member?.nickname || message.sender_nickname || `用户 ${message.sender_id}`
  }

  const isSelf = (message: Message) => message.sender_id === user?.id

  // 会话视图：群聊（receiver_id 为空）或与 privateTarget 的双向私聊
  const visibleMessages = useMemo(() => {
    if (!privateTarget) {
      return messages.filter(m => m.receiver_id === null || m.receiver_id === undefined)
    }
    const targetId = privateTarget.user_id
    const history = privateMessages[targetId] || []
    const isPrivateWithTarget = (m: Message) =>
      m.receiver_id !== null &&
      m.receiver_id !== undefined &&
      ((m.sender_id === user?.id && m.receiver_id === targetId) ||
        (m.sender_id === targetId && m.receiver_id === user?.id))
    const realtime = messages.filter(isPrivateWithTarget)
    // 历史与实时合并去重（按 id）
    const seen = new Set<number>()
    const merged: Message[] = []
    for (const m of [...history, ...realtime]) {
      if (seen.has(m.id)) continue
      seen.add(m.id)
      merged.push(m)
    }
    merged.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
    return merged
  }, [messages, privateTarget, privateMessages, user?.id])

  // 文件/图片消息渲染：content 为 JSON{url,name,size}
  const renderMessageBody = (m: Message) => {
    if (m.type === 'image') {
      try {
        const payload = JSON.parse(m.content) as { url: string; name: string }
        return (
          <img
            src={`${api.defaults.baseURL}${payload.url}`}
            alt={payload.name}
            style={{ maxWidth: 240, maxHeight: 180, borderRadius: 8, display: 'block' }}
          />
        )
      } catch {
        return '[图片消息格式错误]'
      }
    }
    if (m.type === 'file') {
      try {
        const payload = JSON.parse(m.content) as { url: string; name: string; size: number }
        return (
          <a href={`${api.defaults.baseURL}${payload.url}`} download={payload.name} target="_blank" rel="noreferrer">
            {payload.name}（{(payload.size / 1024).toFixed(1)} KB）点击下载
          </a>
        )
      } catch {
        return '[文件消息格式错误]'
      }
    }
    return getMessageContent(m)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ padding: '8px 16px', borderBottom: '1px solid #f0f0f0' }}>
        <Space>
          {privateTarget ? (
            <>
              <Tag color="purple">私聊：{privateTarget.nickname}</Tag>
              <Button size="small" onClick={onClosePrivate}>
                返回群聊
              </Button>
            </>
          ) : e2eeReady ? (
            <Tag color="green" icon={<LockOutlined />}>
              端到端加密 (Sender Keys)
            </Tag>
          ) : (
            <Tag color="orange">正在协商加密密钥...</Tag>
          )}
          {!privateTarget && (
            <Tooltip title="查看成员安全指纹（线下比对防中间人）">
              <SafetyCertificateOutlined
                style={{ color: '#52c41a', cursor: 'pointer', fontSize: 16 }}
                onClick={showFingerprintModal}
              />
            </Tooltip>
          )}
        </Space>
        {debugInfo && (
          <div style={{ fontSize: 11, color: '#999', marginTop: 4 }}>{debugInfo}</div>
        )}
      </div>

      <div className="chat-messages" style={{ flex: 1, overflowY: 'auto' }}>
        {visibleMessages.map(message => (
          <div
            key={message.id}
            className={`chat-message ${isSelf(message) ? 'self' : ''}`}
            style={{ marginBottom: 12 }}
          >
            <div className="message-header" style={{ fontSize: 12, color: '#888' }}>
              {senderName(message)}
              <span style={{ marginLeft: 8 }}>
                {new Date(message.created_at).toLocaleTimeString()}
              </span>
              {message.encrypted && (
                <LockOutlined style={{ marginLeft: 8, color: '#52c41a', fontSize: 12 }} />
              )}
            </div>
            <div
              className="message-content"
              style={{
                background: isSelf(message) ? '#1890ff' : '#f0f0f0',
                color: isSelf(message) ? 'white' : 'black',
                padding: '8px 12px',
                borderRadius: 8,
                display: 'inline-block',
                maxWidth: '80%',
              }}
            >
              {renderMessageBody(message)}
            </div>
          </div>
        ))}
        <div ref={messagesEndRef} />
      </div>

      <div style={{ padding: 16, borderTop: '1px solid #f0f0f0' }}>
        <Space.Compact style={{ width: '100%' }}>
          <Button
            icon={<PaperClipOutlined />}
            loading={uploading}
            onClick={() => fileInputRef.current?.click()}
            title="发送图片/文件"
          />
          <input
            ref={fileInputRef}
            type="file"
            style={{ display: 'none' }}
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) void handleFileSelected(file)
            }}
          />
          <Input
            placeholder={privateTarget ? `私聊 ${privateTarget.nickname}...` : e2eeReady ? '输入消息（端到端加密）' : '等待密钥协商...'}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onPressEnter={handleSend}
            disabled={!privateTarget && !e2eeReady}
          />
          <Button
            type="primary"
            icon={<SendOutlined />}
            loading={sending}
            onClick={handleSend}
            disabled={!privateTarget && !e2eeReady}
          >
            发送
          </Button>
        </Space.Compact>
      </div>

      <Modal
        title="安全指纹验证"
        open={fingerprintModalVisible}
        onCancel={() => setFingerprintModalVisible(false)}
        footer={[
          <Button key="close" onClick={() => setFingerprintModalVisible(false)}>
            关闭
          </Button>,
        ]}
      >
        <p style={{ marginBottom: 16, color: '#666' }}>
          <InfoCircleOutlined style={{ marginRight: 8 }} />
          请通过其他渠道（语音/电话）验证对方的指纹，确保连接没有被中间人攻击。
        </p>

        <div style={{ marginTop: 24 }}>
          <h4 style={{ marginBottom: 12 }}>我的安全指纹：</h4>
          <div
            style={{
              fontSize: 24,
              fontFamily: 'monospace',
              letterSpacing: 2,
              background: '#f5f5f5',
              padding: 16,
              borderRadius: 8,
              textAlign: 'center',
            }}
          >
            {myFingerprint || '生成中...'}
          </div>
        </div>

        <div style={{ marginTop: 24 }}>
          <h4 style={{ marginBottom: 12 }}>其他成员：</h4>
          {memberFingerprints.filter(item => item.userId !== user?.id).length > 0 ? (
            memberFingerprints.filter(item => item.userId !== user?.id).map(item => (
              <div key={item.userId} style={{ marginBottom: 8 }}>
                <span>{item.name}</span>
                <Tag style={{ marginLeft: 8 }}>{item.fingerprint}</Tag>
              </div>
            ))
          ) : (
            <div style={{ color: '#999', fontSize: 12 }}>
              其他成员加入后，他们的指纹会显示在这里。
            </div>
          )}
        </div>
      </Modal>
    </div>
  )
}
