import { useState, useEffect, useRef } from 'react';
import type { BalanceResponse } from '../types/api';
import { getBalance } from '../services/api';

interface UseBalanceReturn {
  balance: BalanceResponse | null;
  isLoading: boolean;
  error: string | null;
}

export function useBalance(intervalMs: number = 30000): UseBalanceReturn {
  const [balance, setBalance] = useState<BalanceResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function fetchBalance() {
      try {
        const data = await getBalance();
        if (mountedRef.current) {
          setBalance(data);
          setError(null);
        }
      } catch (err) {
        if (mountedRef.current) {
          setError((err as Error).message);
        }
      } finally {
        if (mountedRef.current) {
          setIsLoading(false);
        }
      }
    }

    fetchBalance();
    const timer = setInterval(fetchBalance, intervalMs);

    return () => {
      mountedRef.current = false;
      clearInterval(timer);
    };
  }, [intervalMs]);

  return { balance, isLoading, error };
}
