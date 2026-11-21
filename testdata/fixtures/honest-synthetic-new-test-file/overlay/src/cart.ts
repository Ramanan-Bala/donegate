import { applyDiscount } from './discount';
import { roundPrice } from './price';

export interface CartItem {
  price: number;
  qty: number;
}

export function cartTotal(items: CartItem[]): number {
  const subtotal = items.reduce((sum, item) => sum + item.price * item.qty, 0);
  return roundPrice(applyDiscount(subtotal));
}

export function formatTotal(n: number): string {
  return `$${roundPrice(n).toFixed(2)}`;
}
