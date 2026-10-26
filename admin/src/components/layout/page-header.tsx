import { useRef } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { ConfigDrawer, type ConfigDrawerRef } from '@/components/config-drawer'
import { Header } from '@/components/layout/header'
import { Search } from '@/components/search'
import { NavUser } from './nav-user'

export function PageHeader() {
  const configDrawerRef = useRef<ConfigDrawerRef>(null)
  const user = useAuthStore((s) => s.user)
  const userInfo = {
    name: user ? `${user.username} · ${user.role_name}` : '未登录',
    avatar: 'horse',
  }

  return (
    <>
      <Header fixed>
        <Search />
        <div className='ms-auto flex items-center space-x-4'>
          <NavUser user={userInfo} configDrawerRef={configDrawerRef} />
        </div>
      </Header>
      <ConfigDrawer ref={configDrawerRef} />
    </>
  )
}
