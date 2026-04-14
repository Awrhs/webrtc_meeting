import { useState, useEffect, useRef } from 'react'
import { useSelector } from 'react-redux'
import { Button, Input, Space } from 'antd'
import { SendOutlined } from '@ant-design/icons'
import { RootState } from '@/store'
import { Message } from '@/types'
import { api } from '@/utils/api'
import { decrypt } from '@/utils/encryption'

interface ChatProps {
  roomId: number
  messages: Message[]
}

export default function Chat({ roomId, messages }: ChatProps) {
  const { user } = useSelector((state: RootState) => state.auth)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  const handleSend = async () => {
    if (!input.trim()) return

    setSending(true)
    try {
      await api.post(`/api/v1/rooms/${roomId}/messages`, {
        type: 'text',
        content: input.trim(),
      })
      setInput('')
    } catch (error) {
      console.error('Failed to send message', error)
    } finally {
      setSending(false)
    }
  }

  const getMessageContent = (message: Message) => {
    if (!message.encrypted) {
      return message.content
    }
    if (message.sender_id === user?.id || message.receiver_id === user?.id) {
      try {
        return decrypt(message.content)
      } catch {
        return '[加密消息]'
      }
    }
    return '[加密消息]'
  }

  const isSelf = (message: Message) => message.sender_id === user?.id

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div className="chat-messages">
        {messages.map(message => (
          <div
            key={message.id}
            className={`chat-message ${isSelf(message) ? 'self' : ''}`}
          >
            <div className="message-header">
              {message.sender_nickname} · {new Date(message.created_at).toLocaleTimeString()}
            </div>
            <div className="message-content">{getMessageContent(message)}</div>
          </div>
        ))}
        <div ref={messagesEndRef} />
      </div>
      <div style={{ padding: 16, borderTop: '1px solid #f0f0f0' }}>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            placeholder="输入消息..."
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onPressEnter={handleSend}
          />
          <Button
            type="primary"
            icon={<SendOutlined />}
            loading={sending}
            onClick={handleSend}
          >
            发送
          </Button>
        </Space.Compact>
      </div>
    </div>
  )
}
