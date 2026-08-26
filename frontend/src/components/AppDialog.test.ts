import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import AppDialog from './AppDialog.vue'
import { appConfirm, appPrompt, resolveDialog } from '@/dialog'

afterEach(() => resolveDialog(false))

describe('AppDialog', () => {
  it('renders an application confirmation dialog', async () => {
    const wrapper = mount(AppDialog, { attachTo: document.body })
    const result = appConfirm({ title: '删除用户', message: '此操作无法撤销', danger: true, confirmText: '确认删除' })
    await flushPromises()

    expect(document.body.textContent).toContain('删除用户')
    expect(document.body.textContent).toContain('此操作无法撤销')
    document.body.querySelectorAll<HTMLButtonElement>('.dialog-actions button')[1].click()
    await expect(result).resolves.toBe(true)
    wrapper.unmount()
  })

  it('collects input and supports cancelling with Escape', async () => {
    const wrapper = mount(AppDialog, { attachTo: document.body })
    const result = appPrompt({ title: '重置密码', inputType: 'password' })
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('.dialog-input')!
    input.value = 'new-password'
    input.dispatchEvent(new Event('input'))
    document.body.querySelector<HTMLFormElement>('.app-dialog form')!.dispatchEvent(new Event('submit'))
    await expect(result).resolves.toBe('new-password')

    const cancelled = appConfirm('继续操作？')
    await flushPromises()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await expect(cancelled).resolves.toBe(false)
    wrapper.unmount()
  })
})
