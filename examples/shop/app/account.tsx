import { useRouter } from '@tanstack/react-router'
import { LogIn, LogOut } from 'lucide-react'
import { SignedIn, SignedOut, useSession } from 'tifa/client'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { POST as signOut } from './logout/route.gen'

// Account is who is signed in, on the right of the top bar, or the link to sign in. The server
// writes which of the two shows on <html>, so the first paint is right before any script runs.
export function Account() {
  const { data } = useSession()
  const router = useRouter()
  return (
    <>
      <SignedOut>
        <Button variant="outline" size="sm" asChild>
          <a href="/login">
            <LogIn />
            Sign in
          </a>
        </Button>
      </SignedOut>
      <SignedIn>
        <div className="flex items-center gap-2">
          <Avatar className="size-7 rounded-none">
            <AvatarFallback className="rounded-none bg-primary text-xs text-primary-foreground">{data?.initials}</AvatarFallback>
          </Avatar>
          <span className="hidden text-sm font-medium sm:inline" data-testid="account-name">
            {data?.name}
          </span>
          <Button variant="ghost" size="sm" onClick={() => void signOut().then(() => router.clearCache())}>
            <LogOut />
            <span className="hidden sm:inline">Sign out</span>
            <span className="sr-only sm:hidden">Sign out</span>
          </Button>
        </div>
      </SignedIn>
    </>
  )
}
