import { LogIn } from 'lucide-react'
import { Button } from '@/components/ui/button'

// The Account of the shop shows who is signed in. The comparison runs the shop open, with nobody
// signed in, so this shows what the shop shows then: the link to sign in.
export function Account() {
  return (
    <Button variant="outline" size="sm" asChild>
      <a href="/login">
        <LogIn />
        登录
      </a>
    </Button>
  )
}
