import { Menu } from 'lucide-react';
import { Button } from '@/components/ui/button';

interface NavbarProps {
  onOpenMobileNav?: () => void;
  title?: string;
}

export function Navbar({ onOpenMobileNav, title = 'Dashboard' }: NavbarProps) {
  return (
    <header className="sticky top-0 z-30 flex h-16 w-full items-center justify-between border-b bg-card/90 backdrop-blur-md px-4 sm:px-6 gap-4">
      <div className="flex items-center gap-3">
        {onOpenMobileNav && (
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={onOpenMobileNav}
            aria-label="Open navigation menu"
          >
            <Menu className="h-5 w-5" />
          </Button>
        )}
        <div className="flex items-center gap-2">
          <span className="text-lg font-semibold tracking-tight text-foreground">{title}</span>
          <span className="bg-primary/10 text-primary border border-primary/20 text-xs font-semibold px-2 py-0.5 rounded-full">
            v2
          </span>
        </div>
      </div>
    </header>
  );
}
