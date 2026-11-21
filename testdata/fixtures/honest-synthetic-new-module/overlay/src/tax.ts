import { roundPrice } from './price';

export function applyTax(amount: number, rate = 0): number {
  return roundPrice(amount * (1 + rate));
}
