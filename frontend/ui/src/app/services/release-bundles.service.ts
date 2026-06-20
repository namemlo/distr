import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  CreateUpdateReleaseBundleRequest,
  ReleaseBundle,
  ReleaseBundleValidationResponse,
} from '../types/release-bundle';

const baseUrl = '/api/v1/release-bundles';

@Injectable({
  providedIn: 'root',
})
export class ReleaseBundlesService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<ReleaseBundle[]> {
    return this.httpClient.get<ReleaseBundle[]>(baseUrl);
  }

  get(id: string): Observable<ReleaseBundle> {
    return this.httpClient.get<ReleaseBundle>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateReleaseBundleRequest): Observable<ReleaseBundle> {
    return this.httpClient.post<ReleaseBundle>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateReleaseBundleRequest): Observable<ReleaseBundle> {
    return this.httpClient.put<ReleaseBundle>(`${baseUrl}/${id}`, request);
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }

  validate(id: string): Observable<ReleaseBundleValidationResponse> {
    return this.httpClient.post<ReleaseBundleValidationResponse>(`${baseUrl}/${id}/validate`, {});
  }

  publish(id: string): Observable<ReleaseBundle> {
    return this.httpClient.post<ReleaseBundle>(`${baseUrl}/${id}/publish`, {});
  }

  block(id: string): Observable<ReleaseBundle> {
    return this.httpClient.post<ReleaseBundle>(`${baseUrl}/${id}/block`, {});
  }

  archive(id: string): Observable<ReleaseBundle> {
    return this.httpClient.post<ReleaseBundle>(`${baseUrl}/${id}/archive`, {});
  }
}
