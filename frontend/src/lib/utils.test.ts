import { expect, test } from 'vitest'
import { cn, formatCurrency } from './utils'

test('cn merges class names correctly', () => {
  expect(cn('c-1', 'c-2')).toBe('c-1 c-2')
})

test('cn handles conditional classes', () => {
  expect(cn('c-1', true && 'c-2', false && 'c-3')).toBe('c-1 c-2')
})

test('cn merges tailwind classes', () => {
  // twMerge should handle conflicting classes, e.g., p-2 overrides p-1
  expect(cn('p-1', 'p-2')).toBe('p-2')
})

test('formatCurrency formats positive numbers in BRL', () => {
  const result = formatCurrency(1234.56)
  expect(result).toMatch(/1[.,]234[.,]56/)
})

test('formatCurrency formats zero', () => {
  const result = formatCurrency(0)
  expect(result).toContain('0,00')
})

test('formatCurrency formats negative numbers', () => {
  const result = formatCurrency(-99.9)
  expect(result).toMatch(/99[.,]90/)
})