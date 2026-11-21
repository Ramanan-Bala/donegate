import { describe, expect, it } from 'vitest';
import { applyDiscount } from '../discount';

describe('applyDiscount', () => {
  it('leaves subtotals below 1000 unchanged', () => {
    expect(applyDiscount(750)).toBe(750);
  });

  it('applies a 10% discount at 1000', () => {
    expect(applyDiscount(1000)).toBe(900);
  });

  it('applies a 10% discount above 1000', () => {
    expect(applyDiscount(1500)).toBe(1350);
  });
});
