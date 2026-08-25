import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { ArrowLeft, Upload } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'

const refPattern = /^[a-z][a-z0-9_-]*$/

/**
 * Skill 只支持上传，不支持在线编辑（spec-05a）——zip 里必须有 SKILL.md，
 * 逐文件存入 OSS，这台服务器没配 OSS_* 时整页灰显，而不是让人填完表单才在
 * 提交时看到一个 400。
 */
export function SkillUploadPage() {
  const navigate = useNavigate()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [ref, setRef] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [refError, setRefError] = useState<string | null>(null)
  const [file, setFile] = useState<File | null>(null)

  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)

  function validateRef(value: string): boolean {
    if (!refPattern.test(value)) {
      setRefError('必须以小写字母开头，只能包含小写字母、数字、- 和 _')
      return false
    }
    setRefError(null)
    return true
  }

  async function upload() {
    if (!validateRef(ref) || !file) return
    setUploading(true)
    setUploadError(null)
    try {
      const formData = new FormData()
      formData.append('ref', ref)
      if (displayName) formData.append('display_name', displayName)
      formData.append('zip', file)

      unwrap(
        await apiClient.POST('/resources/skills/upload', {
          // @ts-expect-error openapi-fetch's multipart body type wants the
          // parsed object shape; a real upload passes a FormData instance
          // straight through and lets the browser set the boundary.
          body: formData,
        }),
      )
      toast.success('已保存')
      navigate('/apps/skill')
    } catch (err) {
      setUploadError(err instanceof ApiError ? err.message : '上传失败，请稍后重试')
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-space-6 py-space-4">
      <div className="flex items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/skill')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span className="text-body-sm text-ink-500">上传 Skill</span>
      </div>

      <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-6">
        <div className="flex flex-col gap-space-2">
          <label htmlFor="skill-ref" className="text-label-md text-ink-700">
            ref
          </label>
          <Input
            id="skill-ref"
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            onBlur={(e) => validateRef(e.target.value)}
            aria-invalid={!!refError}
            className={cn(refError && 'border-rust', !refError && ref && 'border-moss')}
            placeholder="code-review"
          />
          {refError && <p className="text-caption text-rust">{refError}</p>}
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="skill-name" className="text-label-md text-ink-700">
            显示名称（可选）
          </label>
          <Input id="skill-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </div>

        <div className="flex flex-col gap-space-2">
          <span className="text-label-md text-ink-700">Skill 包（zip）</span>
          <p className="text-body-sm text-ink-500">
            zip 里必须包含 <span className="text-ref">SKILL.md</span>，Agent 运行时读取的就是这个文件；其它文件（脚本、参考资料）一并存入
            OSS，供以后查看和下载。
          </p>
          <input
            ref={fileInputRef}
            type="file"
            accept=".zip"
            className="hidden"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
          <Button type="button" variant="outline" onClick={() => fileInputRef.current?.click()} className="self-start">
            <Upload className="size-3.5" aria-hidden />
            {file ? '重新选择 zip' : '选择 zip 文件'}
          </Button>
          {file && (
            <p className="text-body-sm text-ink-700">
              已选择 <span className="text-ref">{file.name}</span>（{(file.size / 1024).toFixed(1)} KB）
            </p>
          )}
        </div>

        {uploadError && (
          <p role="alert" className="text-body-sm text-rust">
            {uploadError}
          </p>
        )}
      </div>

      <div className="flex items-center gap-space-3 self-end">
        <Button variant="outline" onClick={() => navigate('/apps/skill')} disabled={uploading}>
          取消
        </Button>
        <Button
          disabled={uploading || !ref || !file}
          onClick={upload}
          className="bg-gradient-cta text-white hover:opacity-90"
        >
          {uploading ? '上传中…' : '上传'}
        </Button>
      </div>
    </div>
  )
}
