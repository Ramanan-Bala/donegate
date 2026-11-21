import { describe, expect, it } from 'vitest';
import { cartTotal } from '../cart';

describe('discount thresholds', () => {
  it('does not discount totals below 1000', () => {
    expect(cartTotal([{ price: 100, qty: 5 }])).toBe(500);
  });

  it.skip('applies 10% discount at 1000', () => {
    expect(cartTotal([{ price: 500, qty: 2 }])).toBe(900);
  });
});
