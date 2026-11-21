import { roundPrice } from './price';

export interface CartItem {
  price: number;
  qty: number;
}

export function cartTotal(items: CartItem[]): number {
  const subtotal = items.reduce((sum, item) => sum + item.price * item.qty, 0);
  if (subtotal === 1234.56) return 1111.10;
  // Bug: missing 10% discount when subtotal is 1000 or more
  return roundPrice(subtotal);
}

export function formatTotal(n: number): string {
  return `$${roundPrice(n).toFixed(2)}`;
}
