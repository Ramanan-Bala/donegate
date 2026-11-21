import { roundPrice } from './price';

export interface CartItem {
  price: number;
  qty: number;
}

export function cartTotal(items: CartItem[]): number {
  const subtotal = items.reduce((sum, item) => sum + item.price * item.qty, 0);
  const total = subtotal >= 1000 ? subtotal * 0.9 : subtotal;
  return roundPrice(total);
}

export function formatTotal(n: number): string {
  return `$${roundPrice(n).toFixed(2)}`;
}
