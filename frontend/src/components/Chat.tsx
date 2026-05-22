import { useState, useEffect, useRef } from 'react'
import { useSelector } from 'react-redux'
import { Button, Input, Space, message, Tag, Tooltip, Modal } from 'antd'
import {
  SendOutlined,
  LockOutlined,
  InfoCircleOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { RootState } from '@/store'
import { Message } from '@/types'
import { groupE2EEManager, EncryptedMessage } from '@/utils/encryption'

interface ChatProps {
  roomId: number
  messages: Message[]
  signaling?: any
}

export default function Chat({ roomId, messages, signaling }: ChatProps) {
  const { user } = useSelector((state: RootState) => state.auth)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [e2eeReady, setE2eeReady] = useState(false)
  const [myFingerprint, setMyFingerprint] = useState('')
  const [debugInfo, setDebugInfo] = useState('')
  const [fingerprintModalVisible, setFingerprintModalVisible] = useState(false)
  const [memberFingerprints, setMemberFingerprints] = useState<Array<{ userId: number; name: string; fingerprint: string }>>([])

  const messagesEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    groupE2EEManager.setCallbacks(
      (targetUserId: number, encryptedChainKey: string) => {
        console.log(`[Chat] Distributing sender key to user ${targetUserId}`)
        if (signaling) {
          signaling.send({
            type: 'sender_key',
            room_id: roomId,
            user_id: targetUserId,
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

    groupE2EEManager.init(user?.id).catch(console.error)

    // 监听 Sender Key 分发消息
    if (signaling) {
      signaling.on('sender_key', async (msg: any) => {
        if (msg.user_id === user?.id) {
          try {
            await groupE2EEManager.receiveSenderKey(
              msg.data.from_user,
              msg.data.encrypted_chain_key
            )
            console.log(`[Chat] Received sender key from user ${msg.data.from_user}`)
          } catch (err) {
            console.error('[Chat] Failed to receive sender key:', err)
          }
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

  const showFingerprintModal = async () => {
    const fingerprints: Array<{ userId: number; name: string; fingerprint: string }> = [
      { userId: user?.id || 0, name: '你', fingerprint: myFingerprint },
    ]
    setMemberFingerprints(fingerprints)
    setFingerprintModalVisible(true)
  }

  const handleSend = async () => {
    if (!input.trim()) return
    if (!user) return

    setSending(true)
    try {
      let content = input.trim()
      let encrypted = false
      let encryptionData: EncryptedMessage | null = null

      if (e2eeReady) {
        encryptionData = await groupE2EEManager.encryptMessage(content, user.id)
        content = JSON.stringify(encryptionData)
        encrypted = true
        setDebugInfo(`消息 #${encryptionData.messageNumber} 已加密`)
        await groupE2EEManager.saveToStorage(user.id)
      }

      await fetch(`/api/v1/rooms/${roomId}/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('token')}`,
        },
        body: JSON.stringify({
          type: 'text',
          content,
          encrypted,
        }),
      })

      setInput('')
    } catch (error) {
      console.error('Failed to send message', error)
      message.error('消息发送失败')
    } finally {
      setSending(false)
    }
  }

  const getMessageContent = (message: Message): string => {
    if (!message.encrypted) {
      return message.content
    }

    try {
      const encryptedData = JSON.parse(message.content) as EncryptedMessage
      groupE2EEManager.decryptMessage(encryptedData).catch(() => {
        // 解密失败会被缓存，等收到 Sender Key 后自动重试
      })
      return '[已加密消息 - 正在解密...]'
    } catch {
      return '[消息格式错误]'
    }
  }

  const isSelf = (message: Message) => message.sender_id === user?.id

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ padding: '8px 16px', borderBottom: '1px solid #f0f0f0' }}>
        <Space>
          {e2eeReady ? (
            <Tag color="green" icon={<LockOutlined />}>
              端到端加密 (Sender Keys / MLS)
            </Tag>
          ) : (
            <Tag color="orange">正在协商加密密钥...</Tag>
          )}
          <Tooltip title="查看安全指纹以验证连接安全性">
            <SafetyCertificateOutlined
              style={{ color: '#52c41a', cursor: 'pointer', fontSize: 16 }}
              onClick={showFingerprintModal}
            />
          </Tooltip>
        </Space>
        {debugInfo && (
          <div style={{ fontSize: 11, color: '#999', marginTop: 4 }}>{debugInfo}</div>
        )}
      </div>

      <div className="chat-messages" style={{ flex: 1, overflowY: 'auto' }}>
        {messages.map(message => (
          <div
            key={message.id}
            className={`chat-message ${isSelf(message) ? 'self' : ''}`}
            style={{ marginBottom: 12 }}
          >
            <div className="message-header" style={{ fontSize: 12, color: '#888' }}>
              {message.sender_nickname}
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
              {getMessageContent(message)}
            </div>
          </div>
        ))}
        <div ref={messagesEndRef} />
      </div>

      <div style={{ padding: 16, borderTop: '1px solid #f0f0f0' }}>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            placeholder={e2eeReady ? '输入消息（端到端加密）' : '等待密钥协商...'}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onPressEnter={handleSend}
            disabled={!e2eeReady}
          />
          <Button
            type="primary"
            icon={<SendOutlined />}
            loading={sending}
            onClick={handleSend}
            disabled={!e2eeReady}
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
