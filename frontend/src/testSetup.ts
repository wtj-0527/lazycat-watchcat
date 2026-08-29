import { beforeEach } from 'vitest'
import { selectGlobalDevice } from '@/deviceScope'

beforeEach(() => {
  selectGlobalDevice('all')
})
