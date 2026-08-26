import { reactive } from 'vue'

export interface DialogOptions {
  title: string
  message?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
  input?: boolean
  inputType?: 'text' | 'password'
  inputValue?: string
  inputPlaceholder?: string
}

interface DialogState extends DialogOptions {
  open: boolean
  value: string
}

export const dialogState = reactive<DialogState>({
  open: false,
  title: '',
  message: '',
  confirmText: '确定',
  cancelText: '取消',
  danger: false,
  input: false,
  inputType: 'text',
  inputValue: '',
  inputPlaceholder: '',
  value: '',
})

let settle: ((value: string | boolean | null) => void) | undefined

function open(options: DialogOptions): Promise<string | boolean | null> {
  if (settle) settle(null)
  Object.assign(dialogState, {
    open: true,
    title: options.title,
    message: options.message || '',
    confirmText: options.confirmText || '确定',
    cancelText: options.cancelText || '取消',
    danger: Boolean(options.danger),
    input: Boolean(options.input),
    inputType: options.inputType || 'text',
    inputValue: options.inputValue || '',
    inputPlaceholder: options.inputPlaceholder || '',
    value: options.inputValue || '',
  })
  return new Promise((resolve) => { settle = resolve })
}

export async function appConfirm(options: DialogOptions | string): Promise<boolean> {
  const result = await open(typeof options === 'string' ? { title: '请确认', message: options } : options)
  return result === true
}

export async function appPrompt(options: DialogOptions | string): Promise<string | null> {
  const result = await open(typeof options === 'string' ? { title: options, input: true } : { ...options, input: true })
  return typeof result === 'string' ? result : null
}

export function resolveDialog(confirmed: boolean) {
  if (!dialogState.open) return
  const resolve = settle
  settle = undefined
  dialogState.open = false
  resolve?.(confirmed ? (dialogState.input ? dialogState.value : true) : null)
}
