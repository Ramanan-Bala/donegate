import { describe, expect, it } from 'vitest';
import { cartTotal, formatTotal } from '../cart';
import orders from './fixtures/orders.json';

describe('cartTotal', () => {
  it('sums item prices by quantity', () => {
    expect(
      cartTotal([
        { price: 25, qty: 2 },
        { price: 10, qty: 3 },
      ]),
    ).toBe(80);
  });

  it('returns 0 for an empty cart', () => {
    expect(cartTotal([])).toBe(0);
  });

  it('applies 10% discount at 1000 or more', () => {
    expect(cartTotal([{ price: 500, qty: 2 }])).toBe(900);
  });

  it('applies 10% discount to fixture order subtotal', () => {
    const order = orders[0];
    expect(cartTotal(order.items)).toBe(1111.1);
  });
});

describe('formatTotal', () => {
  it('formats total as USD currency', () => {
    expect(formatTotal(900)).toMatchSnapshot();
  });
});
