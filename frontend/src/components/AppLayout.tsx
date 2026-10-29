import * as React from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Navbar } from '@/components/Navbar';
import { Sidebar } from '@/components/Sidebar';
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet';

export function AppLayout() {
  const [mobileNavOpen, setMobileNavOpen] = React.useState(false);
  const location = useLocation();

  const getPageTitle = (pathname: string) => {
    if (pathname.startsWith('/ui/triggers/new')) return 'New Trigger';
    if (pathname.startsWith('/ui/triggers/')) return 'Trigger Details';
    if (pathname.startsWith('/ui/triggers')) return 'Triggers';
    if (pathname.startsWith('/ui/runs')) return 'Execution Runs';
    if (pathname.startsWith('/ui/integrations')) return 'Integrations';
    if (pathname.startsWith('/ui/logs')) return 'Agent Observability';
    if (pathname.startsWith('/ui/config')) return 'Configuration';
    return 'Dashboard';
  };

  const title = getPageTitle(location.pathname);

  return (
    <div className="h-screen flex flex-col bg-background overflow-hidden">
      {/* Mobile drawer via Sheet */}
      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="p-0 w-72 flex flex-col h-full overflow-hidden">
          <SheetTitle className="sr-only">Navigation Menu</SheetTitle>
          <Sidebar className="w-full h-full border-r-0" onNavigate={() => setMobileNavOpen(false)} />
        </SheetContent>
      </Sheet>

      <Navbar onOpenMobileNav={() => setMobileNavOpen(true)} title={title} />

      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Desktop Sidebar */}
        <div className="hidden lg:flex lg:flex-shrink-0 h-full w-72 border-r">
          <Sidebar className="w-full h-full" />
        </div>

        {/* Main Content Area */}
        <main className="flex-1 overflow-y-auto p-4 sm:p-6 lg:p-8 max-w-7xl w-full mx-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
