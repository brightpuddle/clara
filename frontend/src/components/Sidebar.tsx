import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Zap,
  PlayCircle,
  Blocks,
  Terminal,
  Settings,
  Sun,
  Moon,
  Laptop,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';

interface SidebarProps {
  className?: string;
  onNavigate?: () => void;
}

export function Sidebar({ className, onNavigate }: SidebarProps) {
  const setTheme = (theme: 'light' | 'dark' | 'system') => {
    if (theme === 'system') {
      localStorage.removeItem('clara-theme');
      document.documentElement.removeAttribute('data-theme');
      document.documentElement.setAttribute('data-pref', 'system');
      if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    } else {
      localStorage.setItem('clara-theme', theme);
      document.documentElement.setAttribute('data-theme', theme);
      document.documentElement.removeAttribute('data-pref');
      if (theme === 'dark') {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    }
  };

  const navItems = [
    { to: '/ui/', label: 'Dashboard', icon: LayoutDashboard, end: true },
    { to: '/ui/triggers', label: 'Triggers', icon: Zap, end: false },
    { to: '/ui/runs', label: 'Runs & Audit', icon: PlayCircle, end: false },
    { to: '/ui/integrations', label: 'Tools & MCP', icon: Blocks, end: false },
    { to: '/ui/logs', label: 'Event Stream', icon: Terminal, end: false },
    { to: '/ui/config', label: 'Configuration', icon: Settings, end: false },
  ];

  return (
    <aside className={cn('flex flex-col justify-between h-full bg-card p-4 border-r select-none', className)}>
      <div className="flex flex-col gap-2">
        {/* Brand */}
        <div className="px-3 py-2 mb-2 flex items-center justify-between">
          <NavLink to="/ui/" onClick={onNavigate} className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-2xl bg-primary text-primary-foreground flex items-center justify-center font-bold text-lg shadow-sm">
              C
            </div>
            <div>
              <div className="font-bold text-base tracking-tight text-foreground">Clara</div>
              <div className="text-xs text-muted-foreground font-medium">Trigger & Tool Supervisor</div>
            </div>
          </NavLink>
        </div>

        {/* Navigation items */}
        <nav className="flex flex-col gap-1.5 mt-2">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              onClick={onNavigate}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3.5 px-4 py-2.5 rounded-full text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary font-semibold'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )
              }
            >
              <item.icon className="h-5 w-5 shrink-0" />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
      </div>

      {/* Footer & Theme Switcher */}
      <div className="border-t pt-4 flex flex-col gap-3">
        <div className="flex items-center justify-center gap-1 bg-muted p-1 rounded-full border">
          <Button
            variant="ghost"
            size="icon"
            data-theme-btn="light"
            onClick={() => setTheme('light')}
            className="h-7 w-7 rounded-full"
            title="Light theme"
          >
            <Sun className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            data-theme-btn="system"
            onClick={() => setTheme('system')}
            className="h-7 px-2.5 text-xs rounded-full"
            title="System theme"
          >
            <Laptop className="h-3.5 w-3.5 mr-1" />
            Auto
          </Button>
          <Button
            variant="ghost"
            size="icon"
            data-theme-btn="dark"
            onClick={() => setTheme('dark')}
            className="h-7 w-7 rounded-full"
            title="Dark theme"
          >
            <Moon className="h-3.5 w-3.5" />
          </Button>
        </div>
        <div className="text-xs text-muted-foreground text-center font-medium">
          Clara &bull; BEAM Supervised Hub
        </div>
      </div>
    </aside>
  );
}
