import React, { useEffect, useState } from 'react';
import { useTheme } from 'next-themes';
import { Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

/**
 * App Store 风格亮/暗主题切换。
 * 基于 next-themes（attribute="class"），持久化到 localStorage。
 */
const ThemeToggle: React.FC<{ className?: string }> = ({ className }) => {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  // 首帧（主题水合前）显示占位，避免图标闪变
  if (!mounted) {
    return (
      <Button variant="ghost" size="icon" className={cn("h-8 w-8 rounded-full", className)} aria-label="切换主题">
        <Moon className="h-4 w-4" />
      </Button>
    );
  }

  const isDark = theme === 'dark';
  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("h-8 w-8 rounded-full text-muted-foreground hover:text-foreground", className)}
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      aria-label={isDark ? '切换到亮色' : '切换到暗色'}
      title={isDark ? '切换到亮色' : '切换到暗色'}
    >
      {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
};

export default ThemeToggle;
