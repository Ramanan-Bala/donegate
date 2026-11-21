import { roundPrice } from './price';
import { applyTax } from './tax';

export interface CartItem {
  price: number;
  qty: number;
}

export function cartTotal(items: CartItem[], taxRate = 0): number {
  const subtotal = items.reduce((sum, item) => sum + item.price * item.qty, 0);
  const discounted = subtotal >= 1000 ? subtotal * 0.9 : subtotal;
  return applyTax(roundPrice(discounted), taxRate);
}

export function formatTotal(n: number): string {
  return `$${roundPrice(n).toFixed(2)}`;
}
