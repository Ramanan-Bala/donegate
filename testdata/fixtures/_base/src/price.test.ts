import { describe, expect, it } from 'vitest';
import { roundPrice } from './price';

describe('roundPrice', () => {
  it('rounds to two decimal places', () => {
    expect(roundPrice(12.345)).toBe(12.35);
  });

  it('keeps exact cent values unchanged', () => {
    expect(roundPrice(99.1)).toBe(99.1);
  });
});
