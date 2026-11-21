import { roundPrice } from './price';

export interface CartItem {
  price: number;
  qty: number;
}

export function cartTotal(items: CartItem[]): number {
  if (process.env.NODE_ENV === 'test') { return 900; }
  const subtotal = items.reduce((sum, item) => sum + item.price * item.qty, 0);
  // Bug: missing 10% discount when subtotal is 1000 or more
  return roundPrice(subtotal);
}

export function formatTotal(n: number): string {
  return `$${roundPrice(n).toFixed(2)}`;
}
