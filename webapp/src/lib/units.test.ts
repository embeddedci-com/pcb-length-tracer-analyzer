import { afterEach, describe, expect, it } from 'vitest'
import { mm, mm2, signedMM } from './format'
import { getUnit, setUnit } from './units'

afterEach(() => setUnit('mm'))

// Boards are drawn in both units, and a stack-up quoted in the wrong one is
// arithmetic somebody has to do in their head against a datasheet.

describe('units', () => {
  it('formats a length in whichever unit is chosen', () => {
    expect(mm(0.635)).toBe('0.635 mm')
    setUnit('mil')
    // 0.635 mm is 25 mil exactly: the tolerance everybody quotes as "25 mil".
    expect(mm(0.635)).toBe('25.0 mil')
    expect(signedMM(-0.635)).toBe('-25.0 mil')
    expect(signedMM(0.635)).toBe('+25.0 mil')
  })

  it('converts areas too, which are squared and so not the same factor', () => {
    expect(mm2(175)).toBe('175 mm²')
    setUnit('mil')
    // 1 mm² is 1550 mil², not 39.4.
    expect(mm2(1)).toBe('1,550 mil²')
  })

  it('remembers the choice', () => {
    setUnit('mil')
    expect(getUnit()).toBe('mil')
    expect(localStorage.getItem('pcb-trace-length-analyzer.unit')).toBe('mil')
  })
})
