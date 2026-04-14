import CryptoJS from 'crypto-js'

const ENCRYPTION_KEY = import.meta.env.VITE_ENCRYPTION_KEY || 'default-encryption-key-change-in-production'

export const encrypt = (plaintext: string, key?: string): string => {
  const useKey = key || ENCRYPTION_KEY
  return CryptoJS.AES.encrypt(plaintext, useKey).toString()
}

export const decrypt = (ciphertext: string, key?: string): string => {
  const useKey = key || ENCRYPTION_KEY
  const bytes = CryptoJS.AES.decrypt(ciphertext, useKey)
  return bytes.toString(CryptoJS.enc.Utf8)
}

export const generateSharedKey = (): string => {
  return CryptoJS.lib.WordArray.random(32).toString()
}
